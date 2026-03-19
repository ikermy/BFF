package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
)

// zeroBillingClient — всегда возвращает AllowedTotal=0 (симулирует пустой баланс).
type zeroBillingClient struct{}

func (z *zeroBillingClient) Quote(_ context.Context, _ string, units int, _ string) (domain.QuoteResult, error) {
	return domain.QuoteResult{
		CanProcess:   false,
		Partial:      false,
		Requested:    units,
		AllowedTotal: 0,
		UnitPrice:    0.50,
	}, nil
}
func (z *zeroBillingClient) Block(_ context.Context, _ domain.BlockRequest) error { return nil }
func (z *zeroBillingClient) BlockBatch(_ context.Context, _ string, _ int, _ string) ([]string, error) {
	return nil, nil
}
func (z *zeroBillingClient) Capture(_ context.Context, _ string, _ int) error { return nil }
func (z *zeroBillingClient) Release(_ context.Context, _ string, _ int) error { return nil }

// убеждаемся, что zeroBillingClient реализует порт
var _ ports.BillingClient = (*zeroBillingClient)(nil)

// TestQuoteUseCase_InsufficientFunds — AllowedTotal=0 → 402 INSUFFICIENT_FUNDS с topUpRequired (п.6.3 ТЗ).
func TestQuoteUseCase_InsufficientFunds(t *testing.T) {
	uc := NewQuoteUseCase(&zeroBillingClient{})

	_, err := uc.Execute(context.Background(), "u-1", 100, "US_CA_08292017")
	if err == nil {
		t.Fatal("expected error for AllowedTotal=0")
	}

	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeInsufficientFunds {
		t.Errorf("expected %s, got %s", domain.ErrCodeInsufficientFunds, appErr.Code)
	}
	if appErr.HTTPStatus != 402 {
		t.Errorf("expected HTTP 402, got %d", appErr.HTTPStatus)
	}
	topUp, ok := appErr.Details["topUpRequired"].(float64)
	if !ok {
		t.Fatalf("expected topUpRequired float64 in Details, got %v", appErr.Details)
	}
	// 100 units * $0.50 = $50.00
	if topUp != 50.0 {
		t.Errorf("expected topUpRequired=50.0, got %f", topUp)
	}
}

// TestQuoteUseCase_Partial — AllowedTotal < units → partial: true, shortfall заполнен (п.6.3 ТЗ).
func TestQuoteUseCase_Partial(t *testing.T) {
	// Mock: 30 sub + 20 cred + 50 wallet = 100 max, при units=150 → partial
	billingClient := newMockBillingForQuote(0.50)
	uc := NewQuoteUseCase(billingClient)

	result, err := uc.Execute(context.Background(), "u-1", 150, "US_CA_08292017")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Partial {
		t.Error("expected Partial=true")
	}
	if result.AllowedTotal != 100 {
		t.Errorf("expected AllowedTotal=100, got %d", result.AllowedTotal)
	}
	if result.Shortfall == nil {
		t.Fatal("expected non-nil Shortfall")
	}
	if result.Shortfall.Units != 50 {
		t.Errorf("expected shortfall.units=50, got %d", result.Shortfall.Units)
	}
	// amountRequired = 50 * 0.50 = 25.00
	if result.Shortfall.AmountRequired != 25.0 {
		t.Errorf("expected shortfall.amountRequired=25.0, got %f", result.Shortfall.AmountRequired)
	}
}

func TestQuoteUseCase_PartialDisabled_ReturnsInsufficientFunds(t *testing.T) {
	billingClient := newMockBillingForQuote(0.50)
	uc := NewQuoteUseCase(billingClient).WithPartialSuccessEnabled(false)

	_, err := uc.Execute(context.Background(), "u-1", 150, "US_CA_08292017")
	if err == nil {
		t.Fatal("expected error when partial success is disabled")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeInsufficientFunds || appErr.HTTPStatus != 402 {
		t.Fatalf("expected INSUFFICIENT_FUNDS 402, got %s %d", appErr.Code, appErr.HTTPStatus)
	}
}

// TestQuoteUseCase_FullSuccess — AllowedTotal >= units → partial: false (п.6.3 ТЗ).
func TestQuoteUseCase_FullSuccess(t *testing.T) {
	billingClient := newMockBillingForQuote(0.50)
	uc := NewQuoteUseCase(billingClient)

	result, err := uc.Execute(context.Background(), "u-1", 50, "US_CA_08292017")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Partial {
		t.Error("expected Partial=false for units=50 (within 100 limit)")
	}
	if !result.CanProcess {
		t.Error("expected CanProcess=true")
	}
	if result.AllowedTotal < 50 {
		t.Errorf("expected AllowedTotal >= 50, got %d", result.AllowedTotal)
	}
}

// TestQuoteUseCase_ValidationErrors — пустые параметры → VALIDATION_ERROR (п.5 ТЗ).
func TestQuoteUseCase_ValidationErrors(t *testing.T) {
	billingClient := newMockBillingForQuote(0.50)
	uc := NewQuoteUseCase(billingClient)

	_, err := uc.Execute(context.Background(), "u-1", 0, "US_CA_08292017")
	if err == nil {
		t.Fatal("expected error for units=0")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %v", err)
	}

	_, err = uc.Execute(context.Background(), "u-1", 10, "")
	if err == nil {
		t.Fatal("expected error for empty revision")
	}
	if !errors.As(err, &appErr) || appErr.Code != domain.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %v", err)
	}
}

// TestGenerateUseCase_PreservesInsufficientFundsFromQuote — QuoteUseCase вернул 402,
// GenerateUseCase не перепаковывает в BILLING_ERROR (п.6.3 ТЗ).
func TestGenerateUseCase_PreservesInsufficientFundsFromQuote(t *testing.T) {
	billingClient := &zeroBillingClient{}
	quoteCase := NewQuoteUseCase(billingClient)
	generateCase := NewGenerateUseCase(billingClient, newMockBarcodeForQuote(), newMockEventsForQuote(), quoteCase)

	_, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 100, Confirmed: true, BuildID: "b1", BatchID: "ba1",
	})
	if err == nil {
		t.Fatal("expected error for zero balance")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	// Должен быть INSUFFICIENT_FUNDS 402, а не BILLING_ERROR 503
	if appErr.Code != domain.ErrCodeInsufficientFunds {
		t.Errorf("expected %s (not BILLING_ERROR), got %s", domain.ErrCodeInsufficientFunds, appErr.Code)
	}
	if appErr.HTTPStatus != 402 {
		t.Errorf("expected HTTP 402, got %d", appErr.HTTPStatus)
	}
}

// ─── вспомогательные минимальные моки ────────────────────────────────────────

func newMockBillingForQuote(unitPrice float64) ports.BillingClient {
	// Переиспользуем существующий billing.MockClient через пакетный импорт нельзя (другой пакет),
	// поэтому используем inline-реализацию с тем же waterfall.
	return &simpleBillingMock{unitPrice: unitPrice}
}

type simpleBillingMock struct{ unitPrice float64 }

func (m *simpleBillingMock) Quote(_ context.Context, _ string, units int, _ string) (domain.QuoteResult, error) {
	sub := min(units, 30)
	remaining := units - sub
	cred := min(remaining, 20)
	remaining -= cred
	wallet := min(remaining, 50)
	allowed := sub + cred + wallet
	partial := allowed < units
	result := domain.QuoteResult{
		CanProcess:   allowed > 0,
		Partial:      partial,
		Requested:    units,
		AllowedTotal: allowed,
		UnitPrice:    m.unitPrice,
		BySource: domain.QuoteBreakdown{
			Subscription: domain.SourceBreakdown{Units: sub},
			Credits:      domain.SourceBreakdown{Units: cred},
			Wallet:       domain.SourceBreakdown{Units: wallet, Amount: float64(wallet) * m.unitPrice},
		},
	}
	if partial {
		result.Shortfall = &domain.Shortfall{
			Units:          units - allowed,
			AmountRequired: float64(units-allowed) * m.unitPrice,
		}
	}
	return result, nil
}
func (m *simpleBillingMock) Block(_ context.Context, _ domain.BlockRequest) error { return nil }
func (m *simpleBillingMock) BlockBatch(_ context.Context, _ string, _ int, _ string) ([]string, error) {
	return nil, nil
}
func (m *simpleBillingMock) Capture(_ context.Context, _ string, _ int) error { return nil }
func (m *simpleBillingMock) Release(_ context.Context, _ string, _ int) error { return nil }

func newMockBarcodeForQuote() ports.BarcodeGenClient {
	// Используем barcodegen.MockClient из пакета — нельзя напрямую, поэтому inline.
	return &simpleBarcodeClientForQuote{}
}

type simpleBarcodeClientForQuote struct{}

func (c *simpleBarcodeClientForQuote) Generate(_ context.Context, barcodeType string, _ map[string]any) (domain.BarcodeItem, error) {
	return domain.BarcodeItem{URL: "https://cdn.example.com/test.png", Format: barcodeType}, nil
}
func (c *simpleBarcodeClientForQuote) Calculate(_ context.Context, _, _ string, _ map[string]any) (any, error) {
	return "CALC", nil
}
func (c *simpleBarcodeClientForQuote) Random(_ context.Context, _, _ string, _ map[string]any) (any, error) {
	return "RAND", nil
}
func (c *simpleBarcodeClientForQuote) GeneratePDF417(_ context.Context, _ domain.GeneratePDF417Request) (domain.GeneratePDF417Response, error) {
	return domain.GeneratePDF417Response{}, nil
}
func (c *simpleBarcodeClientForQuote) GenerateCode128(_ context.Context, _ domain.GenerateCode128Request) (domain.GenerateCode128Response, error) {
	return domain.GenerateCode128Response{}, nil
}

func newMockEventsForQuote() ports.EventPublisher {
	return &simpleEventsForQuote{}
}

type simpleEventsForQuote struct{}

func (e *simpleEventsForQuote) PublishSagaCompleted(_ context.Context, _ string) error { return nil }
func (e *simpleEventsForQuote) PublishBulkResult(_ context.Context, _ domain.BulkResultEvent) error {
	return nil
}
func (e *simpleEventsForQuote) PublishPartialCompleted(_ context.Context, _ domain.PartialCompletedEvent) error {
	return nil
}
func (e *simpleEventsForQuote) PublishBarcodeEdited(_ context.Context, _ domain.BarcodeEditedEvent) error {
	return nil
}
func (e *simpleEventsForQuote) PublishBarcodeGenerated(_ context.Context, _ domain.BarcodeGeneratedEvent) error {
	return nil
}
