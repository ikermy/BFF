package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/ikermy/BFF/internal/domain"
)

type bulkBillingClientStub struct {
	blockBatchFn func(ctx context.Context, userID string, count int, batchID string) ([]string, error)
}

func (s *bulkBillingClientStub) Quote(_ context.Context, _ string, _ int, _ string) (domain.QuoteResult, error) {
	return domain.QuoteResult{}, nil
}

func (s *bulkBillingClientStub) Block(_ context.Context, _ domain.BlockRequest) error { return nil }
func (s *bulkBillingClientStub) Capture(_ context.Context, _ string, _ int) error     { return nil }
func (s *bulkBillingClientStub) Release(_ context.Context, _ string, _ int) error     { return nil }

func (s *bulkBillingClientStub) BlockBatch(ctx context.Context, userID string, count int, batchID string) ([]string, error) {
	if s.blockBatchFn != nil {
		return s.blockBatchFn(ctx, userID, count, batchID)
	}
	return []string{"tx-1", "tx-2"}, nil
}

func TestBulkUseCase_ValidateRow_Success(t *testing.T) {
	uc := NewBulkUseCase(&bulkBillingClientStub{})

	resp := uc.ValidateRow(context.Background(), domain.InternalValidateRequest{
		Revision: "US_CA_08292017",
		Fields:   map[string]any{"firstName": "JOHN"},
	})

	if !resp.Valid {
		t.Fatalf("expected valid=true, got %+v", resp)
	}
	if len(resp.Errors) != 0 {
		t.Fatalf("expected no errors, got %+v", resp.Errors)
	}
}

func TestBulkUseCase_ValidateRow_MissingRevisionAndFields(t *testing.T) {
	uc := NewBulkUseCase(&bulkBillingClientStub{})

	resp := uc.ValidateRow(context.Background(), domain.InternalValidateRequest{})
	if resp.Valid {
		t.Fatalf("expected valid=false, got %+v", resp)
	}
	if len(resp.Errors) != 2 {
		t.Fatalf("expected 2 validation errors, got %+v", resp.Errors)
	}
}

func TestBulkUseCase_BlockBatch_ValidationErrors(t *testing.T) {
	uc := NewBulkUseCase(&bulkBillingClientStub{})

	_, err := uc.BlockBatch(context.Background(), "u-1", 0, "batch-1")
	if err == nil {
		t.Fatal("expected validation error for count <= 0")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %s", appErr.Code)
	}

	_, err = uc.BlockBatch(context.Background(), "u-1", 2, "")
	if err == nil {
		t.Fatal("expected validation error for empty batchId")
	}
}

func TestBulkUseCase_BlockBatch_Success(t *testing.T) {
	uc := NewBulkUseCase(&bulkBillingClientStub{
		blockBatchFn: func(_ context.Context, userID string, count int, batchID string) ([]string, error) {
			if userID != "u-1" || count != 2 || batchID != "batch-1" {
				t.Fatalf("unexpected args: userID=%s count=%d batchID=%s", userID, count, batchID)
			}
			return []string{"batch-1-tx-1", "batch-1-tx-2"}, nil
		},
	})

	txIDs, err := uc.BlockBatch(context.Background(), "u-1", 2, "batch-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(txIDs) != 2 {
		t.Fatalf("expected 2 tx ids, got %v", txIDs)
	}
}

func TestBulkUseCase_BlockBatch_WrapsBillingError(t *testing.T) {
	uc := NewBulkUseCase(&bulkBillingClientStub{
		blockBatchFn: func(_ context.Context, _ string, _ int, _ string) ([]string, error) {
			return nil, errors.New("billing unavailable")
		},
	})

	_, err := uc.BlockBatch(context.Background(), "u-1", 2, "batch-1")
	if err == nil {
		t.Fatal("expected BILLING_ERROR")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeBillingError || appErr.HTTPStatus != 503 {
		t.Fatalf("expected BILLING_ERROR 503, got %s %d", appErr.Code, appErr.HTTPStatus)
	}
}
