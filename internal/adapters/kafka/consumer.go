package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/ikermy/BFF/internal/domain"

	"github.com/segmentio/kafka-go"
)

// Consumer — production-реализация BulkJobConsumer (п.14.6 ТЗ).
// Читает сообщения из топика bulk.tasks и передаёт в MessageHandler.
// В dev/test заменяется на MockConsumer.
type Consumer struct {
	reader  *kafka.Reader
	handler MessageHandler
	groupID string
}

// NewConsumer создаёт реальный Kafka consumer для топика bulk.tasks.
// brokerList — запятая-разделённый список брокеров (KAFKA_BROKERS).
// groupID — consumer group (KAFKA_GROUP_ID, п.14.6 ТЗ); дефолт: "bff-bulk-worker".
// Разные среды (staging/prod) должны использовать разные groupID чтобы не конкурировать.
func NewConsumer(brokerList, groupID string, handler MessageHandler) *Consumer {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  strings.Split(brokerList, ","),
		Topic:    TopicBulkJob,
		GroupID:  groupID,
		MinBytes: 1,        // начинаем читать сразу
		MaxBytes: 10 << 20, // 10 МБ — максимальный размер batch-сообщения
	})
	return &Consumer{reader: r, handler: handler, groupID: groupID}
}

// Start — читает сообщения до отмены ctx (п.14.6 ТЗ).
// Offset коммитится автоматически через GroupID после успешного FetchMessage.
// При ошибке хендлера — логируем и продолжаем (dead-letter не реализован).
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
			log.Printf("bulk.tasks consumer: unmarshal error (skipping): offset=%d err=%v",
				m.Offset, err)
			// Коммитим невалидное сообщение чтобы не застрять.
			_ = c.reader.CommitMessages(ctx, m)
			continue
		}

		if err := c.handler(ctx, msg); err != nil {
			log.Printf("bulk.tasks consumer: handler error: batchId=%s items=%d err=%v", msg.BatchID, len(msg.Items), err)
			// Продолжаем — ошибка уже залогирована и bulk.result FAILED опубликован в handler.
		}

		// Коммитим offset только после обработки (at-least-once семантика).
		if err := c.reader.CommitMessages(ctx, m); err != nil {
			log.Printf("bulk.tasks consumer: commit error: offset=%d err=%v", m.Offset, err)
		}
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
