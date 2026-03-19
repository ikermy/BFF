package usecase

import (
	"context"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
)

type BulkUseCase struct {
	billing ports.BillingClient
}

func NewBulkUseCase(billing ports.BillingClient) *BulkUseCase {
	return &BulkUseCase{billing: billing}
}

// ValidateRow — валидирует поля строки от Bulk Service (п.7.1 Bulk_Service_TZ).
// Возвращает InternalValidateResponse; ошибки валидации — в поле Errors, Valid=false.
func (u *BulkUseCase) ValidateRow(_ context.Context, req domain.InternalValidateRequest) domain.InternalValidateResponse {
	var errs []string
	if req.Revision == "" {
		errs = append(errs, domain.NewValidationError("revision is required").Message)
	}
	if len(req.Fields) == 0 {
		errs = append(errs, domain.NewValidationError("fields must not be empty").Message)
	}
	if len(errs) > 0 {
		return domain.InternalValidateResponse{Valid: false, Errors: errs}
	}
	return domain.InternalValidateResponse{Valid: true}
}

// BlockBatch — блокирует средства батчем для Bulk Service.
// Оборачивает ошибки billing в BILLING_ERROR (п.15.1 ТЗ).
func (u *BulkUseCase) BlockBatch(ctx context.Context, userID string, count int, batchID string) ([]string, error) {
	if count <= 0 {
		return nil, domain.NewValidationError("count must be greater than zero")
	}
	if batchID == "" {
		return nil, domain.NewValidationError("batchId is required")
	}
	txIDs, err := u.billing.BlockBatch(ctx, userID, count, batchID)
	if err != nil {
		return nil, domain.NewBillingError(err)
	}
	return txIDs, nil
}
