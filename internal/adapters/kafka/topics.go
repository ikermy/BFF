package kafka

import (
	"context"

	"github.com/ikermy/BFF/internal/domain"
)

// Все Kafka топики BFF — единственный источник истины (п.10.1, п.10.3, п.11.2, п.11.3, п.14.4, п.14.6 ТЗ).
// Используются: Consumer, KafkaPublisher (adapters/events), TopicStore (admin API).
const (
	TopicBulkJob              = "bulk.tasks" // п.14 ТЗ
	TopicBulkResult           = "bulk.result"
	TopicBarcodeGenerated     = "barcode.generated"
	TopicBarcodeEdited        = "barcode.edited"
	TopicSagaCompleted        = "billing.saga.completed"
	TopicSagaPartialCompleted = "billing.saga.partial_completed"
	TopicNotifications        = "notifications.send"
	TopicTransHistory         = "trans-history.log"
	TopicAuthUserInfo         = "auth.user.info"
)

// TopicStore — реестр всех Kafka топиков BFF (п.13.3 ТЗ).
// Реализует ports.KafkaTopicStore; используется admin-эндпоинтом GET /admin/kafka/topics.
// Перенесено из adapters/kafkatopics — константы и реестр теперь в одном пакете.
type TopicStore struct{}

func NewTopicStore() *TopicStore {
	return &TopicStore{}
}

func (s *TopicStore) List(_ context.Context) ([]domain.KafkaTopic, error) {
	return []domain.KafkaTopic{
		// Генерация баркодов (п.10.3, п.10.1 ТЗ)
		{Name: TopicBarcodeGenerated, Enabled: true},
		{Name: TopicBarcodeEdited, Enabled: true},
		// Billing saga (п.14.4 ТЗ)
		{Name: TopicSagaCompleted, Enabled: true},
		{Name: TopicSagaPartialCompleted, Enabled: true},
		// Bulk Service (п.14.6 ТЗ)
		{Name: TopicBulkJob, Enabled: true},
		{Name: TopicBulkResult, Enabled: true},
		// Legacy интеграции (п.11.2, п.11.3, п.11.4 ТЗ)
		{Name: TopicNotifications, Enabled: true},
		{Name: TopicTransHistory, Enabled: true},
		{Name: TopicAuthUserInfo, Enabled: true},
	}, nil
}
