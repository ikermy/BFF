package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ikermy/BFF/internal/domain"

	"github.com/segmentio/kafka-go"
)

// Consumer — production-реализация BulkJobConsumer (п.14.6 ТЗ).
// Читает сообщения из топика bulk.tasks и передаёт в MessageHandler.
// В dev/test заменяется на MockConsumer.
type Consumer struct {
	reader       *kafka.Reader
	handler      MessageHandler
	groupID      string
	dlqPublisher Publisher // опционально; nil — DLQ отключён
}

// NewConsumer создаёт реальный Kafka consumer для топика bulk.tasks.
// brokerList — запятая-разделённый список брокеров (KAFKA_BROKERS).
// groupID — consumer group (KAFKA_GROUP_ID, п.14.6 ТЗ); дефолт: "bff-bulk-worker".
// dlq — публикатор для Dead Letter Queue; nil — DLQ отключён.
// Разные среды (staging/prod) должны использовать разные groupID чтобы не конкурировать.
func NewConsumer(brokerList, groupID string, handler MessageHandler, dlq Publisher) *Consumer {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  strings.Split(brokerList, ","),
		Topic:    TopicBulkJob,
		GroupID:  groupID,
		MinBytes: 1,        // начинаем читать сразу
		MaxBytes: 10 << 20, // 10 МБ — максимальный размер batch-сообщения
	})
	return &Consumer{reader: r, handler: handler, groupID: groupID, dlqPublisher: dlq}
}

// Start — читает сообщения до отмены ctx (п.14.6 ТЗ).
// Offset коммитится автоматически через GroupID после успешного FetchMessage.
// При ошибке (невалидный JSON или ошибка хендлера) — публикуем в DLQ перед коммитом,
// затем коммитим offset и продолжаем (at-least-once, без застревания).
func (c *Consumer) Start(ctx context.Context) error {
	log.Printf("bulk.tasks consumer started (kafka brokers=%s group=%s)",
		c.reader.Config().Brokers, c.groupID)
	defer func() {
		if err := c.reader.Close(); err != nil {
			log.Printf("bulk.tasks consumer: close error: %v", err)
		}
	}()

	for {
		m, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				log.Printf("bulk.tasks consumer stopped (ctx cancelled)")
				return nil
			}
			return fmt.Errorf("bulk.tasks consumer: fetch: %w", err)
		}

		var msg domain.BulkJobMessage
		if err := json.Unmarshal(m.Value, &msg); err != nil {
			log.Printf("bulk.tasks consumer: unmarshal error: offset=%d err=%v", m.Offset, err)
			c.publishDLQ(ctx, m, "unmarshal", err)
			_ = c.reader.CommitMessages(ctx, m)
			continue
		}

		if err := c.handler(ctx, msg); err != nil {
			log.Printf("bulk.tasks consumer: handler error: batchId=%s items=%d err=%v", msg.BatchID, len(msg.Items), err)
			c.publishDLQ(ctx, m, "handler", err)
		}

		// Коммитим offset только после обработки (at-least-once семантика).
		if err := c.reader.CommitMessages(ctx, m); err != nil {
			log.Printf("bulk.tasks consumer: commit error: offset=%d err=%v", m.Offset, err)
		}
	}
}

// publishDLQ публикует сообщение в TopicBulkTasksDLQ.
// Вызывается перед CommitMessages на обоих путях ошибок.
// Если dlqPublisher не задан или публикация упала — только логируем (не блокируем основной поток).
func (c *Consumer) publishDLQ(ctx context.Context, m kafka.Message, reason string, cause error) {
	if c.dlqPublisher == nil {
		return
	}
	rec := DLQRecord{
		OriginalTopic: m.Topic,
		Partition:     m.Partition,
		Offset:        m.Offset,
		RawValue:      m.Value,
		ErrorReason:   reason,
		ErrorDetail:   cause.Error(),
		FailedAt:      time.Now().UTC(),
	}
	if err := c.dlqPublisher.Publish(ctx, TopicBulkTasksDLQ, rec); err != nil {
		log.Printf("bulk.tasks consumer: DLQ publish error: offset=%d err=%v", m.Offset, err)
	} else {
		log.Printf("bulk.tasks consumer: message sent to DLQ: offset=%d reason=%s", m.Offset, reason)
	}
}

// PendingCount — для реального consumer буфера нет, возвращает lag из stats.
// Stats обновляются асинхронно, значение приблизительное.
func (c *Consumer) PendingCount() int {
	stats := c.reader.Stats()
	lag := stats.Lag
	if lag < 0 {
		return 0
	}
	return int(lag)
}
