package events

import (
	"context"
	"log"
	"sync"

	"github.com/ikermy/BFF/internal/domain"
)

// MockPublisher — capturing mock для тестов.
// Реализует EventPublisher + TransHistoryPublisher + NotificationsPublisher.
type MockPublisher struct {
	mu                  sync.Mutex
	GenerationCompletes []domain.NotificationRequest
	GenerationErrors    []domain.ErrorNotificationRequest
	BarcodeEdited       []domain.BarcodeEditedEvent
}

func NewMockPublisher() *MockPublisher {
	return &MockPublisher{}
}

func (p *MockPublisher) PublishSagaCompleted(_ context.Context, sagaID string) error {
	log.Printf("event saga.completed published: sagaId=%s", sagaID)
	return nil
}

func (p *MockPublisher) PublishBulkResult(_ context.Context, event domain.BulkResultEvent) error {
	log.Printf("event bulk.result published: job=%s status=%s", event.JobID, event.Status)
	return nil
}

// PublishPartialCompleted публикует billing.saga.partial_completed (п.14.4 ТЗ).
func (p *MockPublisher) PublishPartialCompleted(_ context.Context, event domain.PartialCompletedEvent) error {
	log.Printf("event billing.saga.partial_completed: sagaId=%s success=%d released=%d",
		event.SagaID, event.SuccessUnits, event.ReleasedUnits)
	return nil
}

// PublishBarcodeEdited публикует barcode.edited после бесплатного редактирования (п.10.1 ТЗ).
func (p *MockPublisher) PublishBarcodeEdited(_ context.Context, event domain.BarcodeEditedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.BarcodeEdited = append(p.BarcodeEdited, event)
	log.Printf("event barcode.edited published: id=%s field=%s newUrl=%s",
		event.ID, event.Field, event.NewURL)
	return nil
}

// PublishBarcodeGenerated публикует barcode.generated после успешной генерации (п.10.3 ТЗ).
// Топик: barcode.generated. Consumer — History Service.
func (p *MockPublisher) PublishBarcodeGenerated(_ context.Context, event domain.BarcodeGeneratedEvent) error {
	log.Printf("event barcode.generated published: userId=%s buildId=%s type=%s url=%s",
		event.UserID, event.BuildID, event.BarcodeType, event.BarcodeURL)
	return nil
}

// LogTransaction имитирует публикацию TRANSACTION_LOG в trans-history.log (п.11.3 ТЗ).
func (p *MockPublisher) LogTransaction(_ context.Context, entry domain.TransactionLog) error {
	log.Printf("[trans-history] TRANSACTION_LOG → userId=%s type=%s amount=%.2f",
		entry.UserID, entry.Type, entry.Amount)
	return nil
}

// ─── NotificationsPublisher interface (п.11.2 ТЗ) ────────────────────────────

// SendGenerationComplete имитирует GENERATION_COMPLETE; захватывает запрос для assertions.
func (p *MockPublisher) SendGenerationComplete(_ context.Context, req domain.NotificationRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.GenerationCompletes = append(p.GenerationCompletes, req)
	log.Printf("[notifications] GENERATION_COMPLETE → userId=%s buildId=%s barcodeCount=%d",
		req.UserID, req.BuildID, req.BarcodeCount)
	return nil
}

// SendGenerationError имитирует GENERATION_ERROR; захватывает запрос для assertions.
func (p *MockPublisher) SendGenerationError(_ context.Context, req domain.ErrorNotificationRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.GenerationErrors = append(p.GenerationErrors, req)
	log.Printf("[notifications] GENERATION_ERROR → userId=%s buildId=%s error=%s",
		req.UserID, req.BuildID, req.Error)
	return nil
}
