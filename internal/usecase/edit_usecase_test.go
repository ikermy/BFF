package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/ikermy/BFF/internal/adapters/barcodegen"
	"github.com/ikermy/BFF/internal/adapters/billing"
	"github.com/ikermy/BFF/internal/adapters/events"
	"github.com/ikermy/BFF/internal/domain"
)

// mockHistoryClient — тестовый mock History Service.
type mockHistoryClient struct {
	canEdit bool
	err     error
}

func (m *mockHistoryClient) CheckFreeEdit(_ context.Context, _ string) (bool, error) {
	return m.canEdit, m.err
}

func (m *mockHistoryClient) GetBarcode(_ context.Context, barcodeID string) (domain.BarcodeRecord, error) {
	if m.err != nil {
		return domain.BarcodeRecord{}, m.err
	}
	return domain.BarcodeRecord{
		ID:          barcodeID,
		UserID:      "mock-user-id",
		Revision:    "US_CA_08292017",
		BarcodeType: "pdf417",
		BarcodeURL:  "https://cdn.example.com/barcodes/test.png",
		Fields:      map[string]any{"firstName": "JOHN"},
		EditFlag:    !m.canEdit,
	}, nil
}

func TestEditUseCase_Success(t *testing.T) {
	uc := NewEditUseCase(
		billing.NewMockClient(0.50),
		barcodegen.NewMockClient(),
		&mockHistoryClient{canEdit: true},
		events.NewMockPublisher(),
	)

	resp, err := uc.Execute(context.Background(), "user-1", "barcode-123", domain.EditRequest{
		Field: "DAC",
		Value: "UPDATED",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.CanEdit {
		t.Error("expected CanEdit=true")
	}
	if resp.NewURL == "" {
		t.Error("expected non-empty NewURL")
	}
}

func TestEditUseCase_AlreadyUsed(t *testing.T) {
	// editFlag=true → право уже использовано → 402
	uc := NewEditUseCase(
		billing.NewMockClient(0.50),
		barcodegen.NewMockClient(),
		&mockHistoryClient{canEdit: false},
		events.NewMockPublisher(),
	)

	resp, err := uc.Execute(context.Background(), "user-1", "barcode-123", domain.EditRequest{
		Field: "DAC",
		Value: "UPDATED",
	})
	if err == nil {
		t.Fatal("expected error when edit already used")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.HTTPStatus != 402 {
		t.Errorf("expected HTTP 402, got %d", appErr.HTTPStatus)
	}
	if resp.CanEdit {
		t.Error("expected CanEdit=false")
	}
	if resp.Reason != "edit_already_used" {
		t.Errorf("expected reason=edit_already_used, got %q", resp.Reason)
	}
}

func TestEditUseCase_ValidationErrors(t *testing.T) {
	uc := NewEditUseCase(
		billing.NewMockClient(0.50),
		barcodegen.NewMockClient(),
		&mockHistoryClient{canEdit: true},
		events.NewMockPublisher(),
	)

	// Пустой barcodeID
	_, err := uc.Execute(context.Background(), "user-1", "", domain.EditRequest{Field: "DAC", Value: "VAL"})
	if err == nil {
		t.Fatal("expected error for empty barcodeID")
	}

	// Пустое поле field
	_, err = uc.Execute(context.Background(), "user-1", "barcode-1", domain.EditRequest{Field: "", Value: "VAL"})
	if err == nil {
		t.Fatal("expected error for empty field")
	}
}

func TestEditUseCase_HistoryClientError(t *testing.T) {
	uc := NewEditUseCase(
		billing.NewMockClient(0.50),
		barcodegen.NewMockClient(),
		&mockHistoryClient{err: errors.New("history service unavailable")},
		events.NewMockPublisher(),
	)

	_, err := uc.Execute(context.Background(), "user-1", "barcode-123", domain.EditRequest{
		Field: "DAC",
		Value: "UPDATED",
	})
	if err == nil {
		t.Fatal("expected error when history service fails")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	// Ошибка history оборачивается как BILLING_ERROR (временно, до появления HISTORY_ERROR)
	if appErr.HTTPStatus != 503 {
		t.Errorf("expected HTTP 503, got %d", appErr.HTTPStatus)
	}
}
