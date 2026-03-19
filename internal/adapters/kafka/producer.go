package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

// Publisher — интерфейс публикации в Kafka топик.
// Позволяет подменять реальный Producer на fake в тестах.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}

// Producer — production-реализация Publisher (п.11.4 ТЗ).
// Использует единственный kafka.Writer на весь жизненный цикл приложения —
// Writer поддерживает пул TCP-соединений к брокерам и переиспользует их.
// Topic НЕ фиксируется в Writer: передаётся в каждом Message (per-message routing).
// Close() должен быть вызван при graceful shutdown для корректного сброса буферов.
type Producer struct {
	writer *kafka.Writer
}

// NewProducer создаёт продюсер с одним shared Writer.
// brokers берётся из cfg.Kafka.Brokers (KAFKA_BROKERS="kafka:9092", п.17.1 ТЗ).
func NewProducer(brokerList string) *Producer {
	return &Producer{
		writer: &kafka.Writer{
			Addr:         kafka.TCP(strings.Split(brokerList, ",")...),
			Balancer:     &kafka.LeastBytes{},
			WriteTimeout: 5 * time.Second,
		},
	}
}

// Publish публикует сообщение в указанный топик.
// Топик задаётся через Message.Topic — Writer без фиксированного топика
// может публиковать в любой топик без пересоздания соединений.
func (p *Producer) Publish(ctx context.Context, topic string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("kafka producer: marshal %s: %w", topic, err)
	}
	return p.writer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Value: body,
	})
}

// Close закрывает Writer и освобождает TCP-соединения к брокерам.
// Вызывается при graceful shutdown приложения (после Run() вернул управление).
func (p *Producer) Close() error {
	return p.writer.Close()
}
