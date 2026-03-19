package kafka

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
	"github.com/ikermy/BFF/internal/usecase"
)

// BulkJobHandler — обрабатывает сообщение bulk.tasks (п.14 ТЗ).
// Одно сообщение содержит items[] — BFF итерирует по каждому и вызывает GenerateUseCase.
type BulkJobHandler struct {
	generate  *usecase.GenerateUseCase
	publisher ports.EventPublisher
}

func NewBulkJobHandler(generate *usecase.GenerateUseCase, publisher ports.EventPublisher) *BulkJobHandler {
	return &BulkJobHandler{generate: generate, publisher: publisher}
}

// Handle — обрабатывает одно bulk.tasks сообщение.
// Для каждого item вызывает generateService.generate({ item, batchId }) — п.14 ТЗ.
func (h *BulkJobHandler) Handle(ctx context.Context, msg domain.BulkJobMessage) error {
	log.Printf("bulk.tasks received: batchId=%s items=%d", msg.BatchID, len(msg.Items))

	var firstErr error
	successTotal := 0

	for _, item := range msg.Items {
		req := domain.GenerateRequest{
			Revision:    item.Revision,
			BarcodeType: "pdf417",
			Units:       1,
			Confirmed:   true,
			BuildID:     fmt.Sprintf("bulk-%s", item.JobID),
			BatchID:     msg.BatchID,
			Fields:      item.Fields,
		}

		result, err := h.generate.Execute(ctx, msg.UserID, req)

		event := domain.BulkResultEvent{
			EventType: "BULK_RESULT",
			JobID:     item.JobID,
			BatchID:   msg.BatchID,
			BuildID:   req.BuildID,
		}

		if err != nil {
			event.Status = "FAILED"
			// FIXME
			// Обработка item уже завершена (ошибка зафиксирована в firstErr).
			// ТЗ не определяет поведение при сбое публикации bulk result события.
			// Намеренно игнорируем.
			_ = h.publisher.PublishBulkResult(ctx, event)
			log.Printf("bulk.tasks item failed: jobId=%s row=%d err=%v", item.JobID, item.RowNumber, err)
			if firstErr == nil {
				firstErr = fmt.Errorf("item jobId=%s: %w", item.JobID, err)
			}
			continue
		}

		event.Status = "COMPLETED"
		event.BarcodeURLs = make(map[string]string, len(result.Barcodes))
		for i, bc := range result.Barcodes {
			event.BarcodeURLs[fmt.Sprintf("row_%d_%d", item.RowNumber, i)] = bc.URL
		}
		// FIXME
		// Генерация item завершена успешно.
		// ТЗ не определяет поведение при сбое публикации bulk result события.
		// Намеренно игнорируем.
		_ = h.publisher.PublishBulkResult(ctx, event)
		successTotal++
	}

	log.Printf("bulk.tasks done: batchId=%s success=%d/%d ts=%s",
		msg.BatchID, successTotal, len(msg.Items), time.Now().UTC().Format(time.RFC3339))
	return firstErr
}
