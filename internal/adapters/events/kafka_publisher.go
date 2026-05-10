package events

import (
	"context"
	"time"

	"github.com/ikermy/BFF/internal/adapters/kafka"
	"github.com/ikermy/BFF/internal/domain"
)

// KafkaPublisher — production-реализация EventPublisher (п.10.3, п.14.4, п.8.3 ТЗ).
// Принимает kafka.Publisher интерфейс — позволяет подменять в тестах без реального брокера.
type KafkaPublisher struct {
	producer kafka.Publisher
}

func NewKafkaPublisher(producer kafka.Publisher) *KafkaPublisher {
	return &KafkaPublisher{producer: producer}
}

// ─── Kafka-сообщения ──────────────────────────────────────────────────────────

type sagaCompletedMsg struct {
	SagaID    string `json:"sagaId"`
	Timestamp string `json:"timestamp"`
}

type sagaPartialCompletedMsg struct {
	SagaID        string `json:"sagaId"`
	SuccessUnits  int    `json:"successUnits"`
	ReleasedUnits int    `json:"releasedUnits"`
	Timestamp     string `json:"timestamp"`
}

// transactionLogMsg — точное соответствие TypeScript-контракту (п.11.3 ТЗ).
type transactionLogMsg struct {
	EventType string                 `json:"eventType"` // "TRANSACTION_LOG"
	UserID    string                 `json:"userId"`
	Type      domain.TransactionType `json:"type"` // GENERATION | PAYMENT | REFUND
	Amount    float64                `json:"amount,omitempty"`
	Details   map[string]any         `json:"details,omitempty"`
	Timestamp string                 `json:"timestamp"`
}

// Notification Service ожидает формат {type, payload} (интерфейс KafkaEvent<T>).
// Старый формат {eventType, userId, channel, data, timestamp} заменён (grpc_kafka_fixes.md §2.1).

// ─── EventPublisher interface ─────────────────────────────────────────────────

// PublishSagaCompleted публикует billing.saga.completed (п.14.4 ТЗ).
// Вызывается когда все баркоды сгенерированы успешно (failedCount == 0).
func (p *KafkaPublisher) PublishSagaCompleted(ctx context.Context, sagaID string) error {
	msg := sagaCompletedMsg{
		SagaID:    sagaID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	return p.producer.Publish(ctx, kafka.TopicSagaCompleted, msg)
}

// PublishPartialCompleted публикует billing.saga.partial_completed (п.14.4 ТЗ).
// Вызывается при частичной генерации: Capture successUnits, Release releasedUnits.
func (p *KafkaPublisher) PublishPartialCompleted(ctx context.Context, event domain.PartialCompletedEvent) error {
	msg := sagaPartialCompletedMsg{
		SagaID:        event.SagaID,
		SuccessUnits:  event.SuccessUnits,
		ReleasedUnits: event.ReleasedUnits,
		Timestamp:     event.Timestamp,
	}
	return p.producer.Publish(ctx, kafka.TopicSagaPartialCompleted, msg)
}

// PublishBarcodeGenerated публикует barcode.generated (п.10.3 ТЗ).
// Consumer — History Service; записывает запись в историю транзакций.
func (p *KafkaPublisher) PublishBarcodeGenerated(ctx context.Context, event domain.BarcodeGeneratedEvent) error {
	return p.producer.Publish(ctx, kafka.TopicBarcodeGenerated, event)
}

// PublishBarcodeEdited публикует barcode.edited (п.10.1 ТЗ).
// Consumer (History Service) устанавливает editFlag=true и обновляет imageUrl.
func (p *KafkaPublisher) PublishBarcodeEdited(ctx context.Context, event domain.BarcodeEditedEvent) error {
	return p.producer.Publish(ctx, kafka.TopicBarcodeEdited, event)
}

// PublishBulkResult публикует bulk.result (п.8.3 Bulk_Service_TZ).
// Consumer — Bulk Service; сигнализирует об окончании обработки задания.
func (p *KafkaPublisher) PublishBulkResult(ctx context.Context, event domain.BulkResultEvent) error {
	return p.producer.Publish(ctx, kafka.TopicBulkResult, event)
}

// LogTransaction публикует TRANSACTION_LOG в trans-history.log (п.11.3 ТЗ).
// Перенесено из adapters/transhistory — все Kafka-отправки в одном месте.
func (p *KafkaPublisher) LogTransaction(ctx context.Context, entry domain.TransactionLog) error {
	msg := transactionLogMsg{
		EventType: "TRANSACTION_LOG",
		UserID:    entry.UserID,
		Type:      entry.Type,
		Amount:    entry.Amount,
		Details:   entry.Details,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	return p.producer.Publish(ctx, kafka.TopicTransHistory, msg)
}

// ─── NotificationsPublisher interface (п.11.2 ТЗ) ────────────────────────────

// SendGenerationComplete публикует GENERATION_COMPLETE в notif-events (grpc_kafka_fixes.md §2.1).
// Формат: {type, payload} — KafkaEvent<T> интерфейс Notification Service.
func (p *KafkaPublisher) SendGenerationComplete(ctx context.Context, req domain.NotificationRequest) error {
	msg := map[string]any{
		"type": "GENERATION_COMPLETE",
		"payload": map[string]any{
			"userId":       req.UserID,
			"channel":      "email",
			"barcodeCount": req.BarcodeCount,
			"buildId":      req.BuildID,
			"downloadUrl":  req.DownloadURL,
			"timestamp":    time.Now().UTC().Format(time.RFC3339),
		},
	}
	return p.producer.Publish(ctx, kafka.TopicNotifications, msg)
}

// SendGenerationError публикует GENERATION_ERROR в notif-events (grpc_kafka_fixes.md §2.1).
// Формат: {type, payload} — KafkaEvent<T> интерфейс Notification Service.
func (p *KafkaPublisher) SendGenerationError(ctx context.Context, req domain.ErrorNotificationRequest) error {
	msg := map[string]any{
		"type": "GENERATION_ERROR",
		"payload": map[string]any{
			"userId":    req.UserID,
			"channel":   "push",
			"error":     req.Error,
			"buildId":   req.BuildID,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		},
	}
	return p.producer.Publish(ctx, kafka.TopicNotifications, msg)
}
