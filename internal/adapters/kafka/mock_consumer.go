package kafka

import (
	"context"
	"log"
	"sync/atomic"

	"github.com/ikermy/BFF/internal/domain"
)

// MessageHandler — функция обработки одного bulk.tasks сообщения.
type MessageHandler func(ctx context.Context, msg domain.BulkJobMessage) error

// MockConsumer — mock Kafka consumer для топика bulk.tasks (п.14.6 ТЗ).
// В production заменяется на реализацию с segmentio/kafka-go.
type MockConsumer struct {
	messages chan domain.BulkJobMessage
	handler  MessageHandler
	pending  atomic.Int64
}

func NewMockConsumer(handler MessageHandler, bufSize int) *MockConsumer {
	return &MockConsumer{
		messages: make(chan domain.BulkJobMessage, bufSize),
		handler:  handler,
	}
}

// Start — начинает читать сообщения из канала до отмены ctx.
func (c *MockConsumer) Start(ctx context.Context) error {
	log.Printf("bulk.tasks consumer started (mock)")
	for {
		select {
		case <-ctx.Done():
			log.Printf("bulk.tasks consumer stopped")
			return nil
		case msg := <-c.messages:
			c.pending.Add(-1)
			if err := c.handler(ctx, msg); err != nil {
				log.Printf("bulk.tasks handler error: batchId=%s items=%d err=%v", msg.BatchID, len(msg.Items), err)
			}
		}
	}
}

// PendingCount — количество сообщений в буфере (для wake endpoint).
func (c *MockConsumer) PendingCount() int {
	return int(c.pending.Load())
}

// Enqueue — добавляет сообщение в буфер (используется в тестах и wake-сценариях).
func (c *MockConsumer) Enqueue(msg domain.BulkJobMessage) {
	c.pending.Add(1)
	c.messages <- msg
}
