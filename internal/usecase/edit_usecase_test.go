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

// ...existing code...

// failingBillingClient — mock BillingClient, у которого Block всегда возвращает ошибку.
// Используется для проверки того, что Edit flow прерывается при сбое биллинга.
type failingBillingClient struct{}

func (f *failingBillingClient) Quote(_ context.Context, _ string, _ int, _ string) (domain.QuoteResult, error) {
	return domain.QuoteResult{}, nil
}
func (f *failingBillingClient) Block(_ context.Context, _ domain.BlockRequest) error {
	return errors.New("billing service unavailable")
}
func (f *failingBillingClient) BlockBatch(_ context.Context, _ string, _ int, _ string) ([]string, error) {
	return nil, nil
}
func (f *failingBillingClient) Capture(_ context.Context, _ string, _ int) error { return nil }
func (f *failingBillingClient) Release(_ context.Context, _ string, _ int) error { return nil }

// countingBarcodeClient считает вызовы Generate — используется чтобы доказать,
// что при падении Block генерация баркода не происходит.
type countingBarcodeClient struct {
	calls int
}

func (c *countingBarcodeClient) Calculate(_ context.Context, _, _ string, _ map[string]any) (any, error) {
	return nil, nil
}
func (c *countingBarcodeClient) Random(_ context.Context, _, _ string, _ map[string]any) (any, error) {
	return nil, nil
}
func (c *countingBarcodeClient) GeneratePDF417(_ context.Context, _ domain.GeneratePDF417Request) (domain.GeneratePDF417Response, error) {
	c.calls++
	return domain.GeneratePDF417Response{Success: true, BarcodeURL: "https://cdn.example.com/barcodes/test.png"}, nil
}
func (c *countingBarcodeClient) GenerateCode128(_ context.Context, _ domain.GenerateCode128Request) (domain.GenerateCode128Response, error) {
	c.calls++
	return domain.GenerateCode128Response{Success: true, BarcodeURL: "https://cdn.example.com/barcodes/test.png"}, nil
}

// TestEditUseCase_BillingBlockFails проверяет, что при падении billing.Block:
// 1. Edit flow немедленно прерывается с BILLING_ERROR 503.
// 2. Генерация баркода НЕ происходит.
// 3. barcode.edited НЕ публикуется (editFlag не сгорает впустую).
// Воспроизводит split-brain сценарий: без этой проверки History ставит editFlag=true,
// а Billing не знает о саге (п.10.1 ТЗ).
func TestEditUseCase_BillingBlockFails(t *testing.T) {
	barcodeClient := &countingBarcodeClient{}
	publisher := events.NewMockPublisher()

	uc := NewEditUseCase(
		&failingBillingClient{},
		barcodeClient,
		&mockHistoryClient{canEdit: true},
		publisher,
	)

	_, err := uc.Execute(context.Background(), "user-1", "barcode-123", domain.EditRequest{
		Field: "DAC",
		Value: "UPDATED",
	})
	if err == nil {
		t.Fatal("expected BILLING_ERROR when Block fails, got nil")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T: %v", err, err)
	}
	if appErr.Code != domain.ErrCodeBillingError {
		t.Errorf("expected BILLING_ERROR, got %s", appErr.Code)
	}
	if appErr.HTTPStatus != 503 {
		t.Errorf("expected HTTP 503, got %d", appErr.HTTPStatus)
	}

	// Генерация баркода должна была быть прервана до вызова BarcodeGen.
	if barcodeClient.calls != 0 {
		t.Errorf("expected 0 BarcodeGen calls after Block failure, got %d", barcodeClient.calls)
	}

	// barcode.edited не должно быть опубликовано — editFlag не должен сгореть.
	if len(publisher.BarcodeEdited) != 0 {
		t.Errorf("expected 0 barcode.edited events after Block failure, got %d", len(publisher.BarcodeEdited))
	}
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
