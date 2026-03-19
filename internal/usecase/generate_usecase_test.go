package usecase

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikermy/BFF/internal/adapters/barcodegen"
	"github.com/ikermy/BFF/internal/adapters/billing"
	"github.com/ikermy/BFF/internal/adapters/events"
	"github.com/ikermy/BFF/internal/adapters/revisions"
	"github.com/ikermy/BFF/internal/domain"
)

// baseBarcodeClient provides shared Calculate/Random used by test barcode clients.
type baseBarcodeClient struct{}

func (b *baseBarcodeClient) Calculate(_ context.Context, _, _ string, _ map[string]any) (any, error) {
	return "CALC_VALUE", nil
}

func (b *baseBarcodeClient) Random(_ context.Context, _, _ string, _ map[string]any) (any, error) {
	return "RANDOM_VALUE", nil
}

// TestMain обнуляет retry-задержки для ускорения тестов.
func TestMain(m *testing.M) {
	barcodeGenRetryDelays = []time.Duration{0, 0, 0}
	m.Run()
}

// assertAppError asserts that err is a *domain.AppError and returns it.
func assertAppError(t *testing.T, err error) *domain.AppError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected *domain.AppError, got nil")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T: %v", err, err)
	}
	return appErr
}

// failingBarcodeClient — падает после successLimit успешных генераций.
type failingBarcodeClient struct {
	baseBarcodeClient
	successLimit int
	count        atomic.Int64
}

func (c *failingBarcodeClient) next(barcodeType string) (domain.BarcodeItem, error) {
	n := int(c.count.Add(1))
	if n > c.successLimit {
		return domain.BarcodeItem{}, errors.New("barcodegen: status 503: unavailable")
	}
	return domain.BarcodeItem{
		URL:    fmt.Sprintf("https://cdn.example.com/barcodes/%s_%d.png", barcodeType, n),
		Format: barcodeType,
	}, nil
}

func (c *failingBarcodeClient) GeneratePDF417(_ context.Context, _ domain.GeneratePDF417Request) (domain.GeneratePDF417Response, error) {
	item, err := c.next("pdf417")
	if err != nil {
		return domain.GeneratePDF417Response{}, err
	}
	return domain.GeneratePDF417Response{Success: true, BarcodeURL: item.URL, Format: item.Format}, nil
}

func (c *failingBarcodeClient) GenerateCode128(_ context.Context, _ domain.GenerateCode128Request) (domain.GenerateCode128Response, error) {
	item, err := c.next("code128")
	if err != nil {
		return domain.GenerateCode128Response{}, err
	}
	return domain.GenerateCode128Response{Success: true, BarcodeURL: item.URL, Format: item.Format}, nil
}

type retryThenSuccessBarcodeClient struct {
	baseBarcodeClient
	failuresLeft atomic.Int64
	count        atomic.Int64
}

// ... Calculate and Random are provided by baseBarcodeClient

func (c *retryThenSuccessBarcodeClient) GeneratePDF417(_ context.Context, _ domain.GeneratePDF417Request) (domain.GeneratePDF417Response, error) {
	call := c.count.Add(1)
	if c.failuresLeft.Load() > 0 {
		c.failuresLeft.Add(-1)
		return domain.GeneratePDF417Response{}, errors.New("barcodegen: status 503: temporary failure")
	}
	return domain.GeneratePDF417Response{Success: true, BarcodeURL: fmt.Sprintf("https://cdn.example.com/barcodes/pdf417_retry_%d.png", call), Format: "pdf417"}, nil
}

func (c *retryThenSuccessBarcodeClient) GenerateCode128(_ context.Context, _ domain.GenerateCode128Request) (domain.GenerateCode128Response, error) {
	call := c.count.Add(1)
	if c.failuresLeft.Load() > 0 {
		c.failuresLeft.Add(-1)
		return domain.GenerateCode128Response{}, errors.New("barcodegen: status 503: temporary failure")
	}
	return domain.GenerateCode128Response{Success: true, BarcodeURL: fmt.Sprintf("https://cdn.example.com/barcodes/code128_retry_%d.png", call), Format: "code128"}, nil
}

type nonRetryableBarcodeClient struct {
	baseBarcodeClient
	count atomic.Int64
}

func (c *nonRetryableBarcodeClient) GeneratePDF417(_ context.Context, _ domain.GeneratePDF417Request) (domain.GeneratePDF417Response, error) {
	c.count.Add(1)
	return domain.GeneratePDF417Response{}, errors.New("barcodegen: status 400: invalid payload")
}

func (c *nonRetryableBarcodeClient) GenerateCode128(_ context.Context, _ domain.GenerateCode128Request) (domain.GenerateCode128Response, error) {
	c.count.Add(1)
	return domain.GenerateCode128Response{}, errors.New("barcodegen: status 400: invalid payload")
}

func TestGenerateUseCase_PartialRequiresConfirmation(t *testing.T) {
	billingClient := billing.NewMockClient(0.50)
	quoteCase := NewQuoteUseCase(billingClient)
	generateCase := NewGenerateUseCase(billingClient, barcodegen.NewMockClient(), events.NewMockPublisher(), quoteCase)

	_, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 150, Confirmed: false, BuildID: "b1", BatchID: "ba1",
	})
	if err == nil {
		t.Fatal("expected error for partial without confirmation")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodePartialFunds {
		t.Fatalf("expected %s, got %s", domain.ErrCodePartialFunds, appErr.Code)
	}
}

func TestGenerateUseCase_InsufficientFunds(t *testing.T) {
	billingClient := billing.NewMockClient(0.50)
	quoteCase := NewQuoteUseCase(billingClient)
	generateCase := NewGenerateUseCase(billingClient, barcodegen.NewMockClient(), events.NewMockPublisher(), quoteCase)

	_, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", Units: 0,
	})
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeValidation || appErr.HTTPStatus != 400 {
		t.Fatalf("expected VALIDATION_ERROR 400, got %s %d", appErr.Code, appErr.HTTPStatus)
	}
}

func TestGenerateUseCase_Success(t *testing.T) {
	billingClient := billing.NewMockClient(1.0) // 100% баланса доступно
	quoteCase := NewQuoteUseCase(billingClient)
	generateCase := NewGenerateUseCase(billingClient, barcodegen.NewMockClient(), events.NewMockPublisher(), quoteCase)

	result, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 10, Confirmed: true, BuildID: "b1", BatchID: "ba1",
		Fields: map[string]any{"firstName": "JOHN"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Barcodes) != 10 {
		t.Fatalf("expected 10 barcodes, got %d", len(result.Barcodes))
	}
}

// TestGenerateUseCase_CompensatingTransactions — п.14.4 ТЗ:
// BarcodeGen упал на 76-м → получаем 75 баркодов, Capture 75, Release 25.
func TestGenerateUseCase_CompensatingTransactions(t *testing.T) {
	billingClient := billing.NewMockClient(1.0) // 100% баланса — billing не ограничивает
	quoteCase := NewQuoteUseCase(billingClient)
	generateCase := NewGenerateUseCase(
		billingClient,
		&failingBarcodeClient{successLimit: 75},
		events.NewMockPublisher(),
		quoteCase,
	)

	result, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 100, Confirmed: true, BuildID: "b1", BatchID: "ba1",
	})
	if err != nil {
		t.Fatalf("unexpected error on partial generation: %v", err)
	}
	if len(result.Barcodes) != 75 {
		t.Fatalf("expected 75 barcodes after compensating, got %d", len(result.Barcodes))
	}
}

// TestGenerateUseCase_AllFailed — все генерации упали → Release всего, BARCODEGEN_ERROR.
func TestGenerateUseCase_AllFailed(t *testing.T) {
	billingClient := billing.NewMockClient(0.50)
	quoteCase := NewQuoteUseCase(billingClient)
	generateCase := NewGenerateUseCase(
		billingClient,
		&failingBarcodeClient{successLimit: 0},
		events.NewMockPublisher(),
		quoteCase,
	)

	_, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 10, Confirmed: true, BuildID: "b1", BatchID: "ba1",
	})
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeBarcodeGenError {
		t.Fatalf("expected %s, got %s", domain.ErrCodeBarcodeGenError, appErr.Code)
	}
	if appErr.HTTPStatus != 503 {
		t.Fatalf("expected HTTP 503, got %d", appErr.HTTPStatus)
	}
}

func TestGenerateUseCase_RetryOn503ThenSuccess(t *testing.T) {
	billingClient := billing.NewMockClient(1.0) // полный доступ — тест проверяет retry BarcodeGen
	quoteCase := NewQuoteUseCase(billingClient)
	barcodeClient := &retryThenSuccessBarcodeClient{}
	barcodeClient.failuresLeft.Store(2)
	generateCase := NewGenerateUseCase(billingClient, barcodeClient, events.NewMockPublisher(), quoteCase)

	result, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 1, Confirmed: true, BuildID: "retry-1", BatchID: "ba1",
	})
	if err != nil {
		t.Fatalf("expected eventual success after retries, got %v", err)
	}
	if len(result.Barcodes) != 1 {
		t.Fatalf("expected 1 barcode, got %d", len(result.Barcodes))
	}
	if barcodeClient.count.Load() != 3 {
		t.Fatalf("expected 3 attempts (2 retries + success), got %d", barcodeClient.count.Load())
	}
}

func TestGenerateUseCase_DoesNotRetryOn400(t *testing.T) {
	billingClient := billing.NewMockClient(1.0) // полный доступ — тест проверяет retry BarcodeGen
	quoteCase := NewQuoteUseCase(billingClient)
	barcodeClient := &nonRetryableBarcodeClient{}
	generateCase := NewGenerateUseCase(billingClient, barcodeClient, events.NewMockPublisher(), quoteCase)

	_, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 1, Confirmed: true, BuildID: "noretry-1", BatchID: "ba1",
	})
	if err == nil {
		t.Fatal("expected BARCODEGEN_ERROR")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeBarcodeGenError {
		t.Fatalf("expected BARCODEGEN_ERROR, got %s", appErr.Code)
	}
	if barcodeClient.count.Load() != 1 {
		t.Fatalf("expected no retry on 400, got %d attempts", barcodeClient.count.Load())
	}
}

// ─── AI Service тесты (п.9.3 ТЗ) ────────────────────────────────────────────
// ─── п.5.2 Валидация минимального набора ─────────────────────────────────────

// TestGenerateUseCase_MinSetValidation_MissingRequired — отсутствуют обязательные поля
// → VALIDATION_ERROR 400 с details.missingFields (п.5.2 ТЗ).
func TestGenerateUseCase_MinSetValidation_MissingRequired(t *testing.T) {
	billingClient := billing.NewMockClient(0.50)
	quoteCase := NewQuoteUseCase(billingClient)
	revStore := revisions.NewMemoryStore() // RequiredInputFields: [firstName, lastName, dateOfBirth]
	generateCase := NewGenerateUseCase(billingClient, barcodegen.NewMockClient(), events.NewMockPublisher(), quoteCase).
		WithRevisionStore(revStore)

	_, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 1, Confirmed: true, BuildID: "b1", BatchID: "ba1",
		Fields: map[string]any{"firstName": "JOHN"}, // lastName и dateOfBirth отсутствуют
	})
	if err == nil {
		t.Fatal("expected VALIDATION_ERROR for missing required fields, got nil")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T: %v", err, err)
	}
	if appErr.Code != domain.ErrCodeValidation || appErr.HTTPStatus != 400 {
		t.Fatalf("expected VALIDATION_ERROR 400, got %s %d", appErr.Code, appErr.HTTPStatus)
	}
	missing, ok := appErr.Details["missingFields"].([]string)
	if !ok || len(missing) == 0 {
		t.Fatalf("expected non-empty missingFields in details, got: %v", appErr.Details)
	}
	missingSet := make(map[string]bool, len(missing))
	for _, f := range missing {
		missingSet[f] = true
	}
	for _, expected := range []string{"lastName", "dateOfBirth"} {
		if !missingSet[expected] {
			t.Errorf("expected %q in missingFields, got: %v", expected, missing)
		}
	}
}

// TestGenerateUseCase_MinSetValidation_AllPresent — все обязательные поля переданы
// → валидация проходит, генерация запускается (п.5.2 ТЗ).
func TestGenerateUseCase_MinSetValidation_AllPresent(t *testing.T) {
	billingClient := billing.NewMockClient(1.0) // полный доступ — тест проверяет только валидацию полей
	quoteCase := NewQuoteUseCase(billingClient)
	revStore := revisions.NewMemoryStore()
	generateCase := NewGenerateUseCase(billingClient, barcodegen.NewMockClient(), events.NewMockPublisher(), quoteCase).
		WithRevisionStore(revStore)

	result, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 2, Confirmed: true, BuildID: "b1", BatchID: "ba1",
		Fields: map[string]any{
			"firstName":   "JOHN",
			"lastName":    "DOE",
			"dateOfBirth": "1990-01-15",
		},
	})
	if err != nil {
		t.Fatalf("all required fields present — unexpected error: %v", err)
	}
	if len(result.Barcodes) != 2 {
		t.Fatalf("expected 2 barcodes, got %d", len(result.Barcodes))
	}
}

// TestGenerateUseCase_MinSetValidation_EmptyString — пустая строка считается отсутствующей (п.5.2 ТЗ).
func TestGenerateUseCase_MinSetValidation_EmptyString(t *testing.T) {
	billingClient := billing.NewMockClient(1.0) // полный доступ — тест проверяет только валидацию
	quoteCase := NewQuoteUseCase(billingClient)
	revStore := revisions.NewMemoryStore()
	generateCase := NewGenerateUseCase(billingClient, barcodegen.NewMockClient(), events.NewMockPublisher(), quoteCase).
		WithRevisionStore(revStore)

	_, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 1, Confirmed: true, BuildID: "b1", BatchID: "ba1",
		Fields: map[string]any{
			"firstName":   "JOHN",
			"lastName":    "   ", // только пробелы — считается пустым
			"dateOfBirth": "1990-01-15",
		},
	})
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("whitespace-only value must be rejected, got: %v", err)
	}
	if appErr.Code != domain.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %s", appErr.Code)
	}
}

// ─── п.11.2 Notifications capturing-тест ─────────────────────────────────────

// TestGenerateUseCase_SendsNotification_OnSuccess — успешная генерация отправляет
// GENERATION_COMPLETE уведомление с корректными полями (п.11.2 ТЗ).
func TestGenerateUseCase_SendsNotification_OnSuccess(t *testing.T) {
	billingClient := billing.NewMockClient(1.0) // 100% — тест проверяет уведомления, не billing
	quoteCase := NewQuoteUseCase(billingClient)
	notifPublisher := events.NewMockPublisher()
	revStore := revisions.NewMemoryStore()

	generateCase := NewGenerateUseCase(billingClient, barcodegen.NewMockClient(), events.NewMockPublisher(), quoteCase).
		WithRevisionStore(revStore).
		WithNotifications(notifPublisher)

	_, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 3, Confirmed: true, BuildID: "b-notify", BatchID: "ba1",
		Fields: map[string]any{
			"firstName": "JOHN", "lastName": "DOE", "dateOfBirth": "1990-01-15",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(notifPublisher.GenerationCompletes) != 1 {
		t.Fatalf("expected 1 GENERATION_COMPLETE notification, got %d", len(notifPublisher.GenerationCompletes))
	}
	notif := notifPublisher.GenerationCompletes[0]
	if notif.UserID != "u-1" {
		t.Errorf("notification.UserID: expected u-1, got %s", notif.UserID)
	}
	if notif.BuildID != "b-notify" {
		t.Errorf("notification.BuildID: expected b-notify, got %s", notif.BuildID)
	}
	if notif.BarcodeCount != 3 {
		t.Errorf("notification.BarcodeCount: expected 3, got %d", notif.BarcodeCount)
	}
	if len(notifPublisher.GenerationErrors) != 0 {
		t.Errorf("expected no GENERATION_ERROR notifications, got %d", len(notifPublisher.GenerationErrors))
	}
}

type mockAIClient struct {
	sigErr   error
	photoErr error
}

func (m *mockAIClient) GenerateSignature(_ context.Context, req domain.AISignatureRequest) (domain.AISignatureResponse, error) {
	if m.sigErr != nil {
		return domain.AISignatureResponse{}, m.sigErr
	}
	return domain.AISignatureResponse{
		ImageURL: "https://cdn.example.com/ai/sig_test.png",
		Style:    req.Style,
	}, nil
}

func (m *mockAIClient) GeneratePhoto(_ context.Context, _ domain.AIPhotoRequest) (domain.AIPhotoResponse, error) {
	if m.photoErr != nil {
		return domain.AIPhotoResponse{}, m.photoErr
	}
	return domain.AIPhotoResponse{ImageURL: "https://cdn.example.com/ai/photo_test.png"}, nil
}

// TestGenerateUseCase_AI_Signature_ExplicitFlag — generateSignature=true → подпись генерируется (п.9.3 ТЗ).
func TestGenerateUseCase_AI_Signature_ExplicitFlag(t *testing.T) {
	billingClient := billing.NewMockClient(1.0) // полный доступ — тест проверяет AI
	quoteCase := NewQuoteUseCase(billingClient)
	generateCase := NewGenerateUseCase(billingClient, barcodegen.NewMockClient(), events.NewMockPublisher(), quoteCase).
		WithAI(&mockAIClient{})

	result, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 1, Confirmed: true, BuildID: "b1", BatchID: "ba1",
		GenerateSignature: true,
		Fields:            map[string]any{"firstName": "JOHN", "lastName": "DOE"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// signatureUrl должен оказаться в полях переданных в BarcodeGen
	_ = result // основная проверка — отсутствие ошибки и успешная генерация
	if !result.Success {
		t.Error("expected success=true")
	}
}

// TestGenerateUseCase_AI_Photo_ExplicitFlag — generatePhoto=true → фото генерируется (п.9.3 ТЗ).
func TestGenerateUseCase_AI_Photo_ExplicitFlag(t *testing.T) {
	billingClient := billing.NewMockClient(1.0) // полный доступ — тест проверяет AI
	quoteCase := NewQuoteUseCase(billingClient)
	generateCase := NewGenerateUseCase(billingClient, barcodegen.NewMockClient(), events.NewMockPublisher(), quoteCase).
		WithAI(&mockAIClient{})

	result, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 1, Confirmed: true, BuildID: "b1", BatchID: "ba1",
		GeneratePhoto:    true,
		PhotoDescription: "young man",
		Gender:           "male",
		Age:              30,
		Fields:           map[string]any{"firstName": "JOHN", "signatureUrl": "existing"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
}

// TestGenerateUseCase_AI_SignatureError_Explicit — AI упал при явном generateSignature=true → 503 (п.9.3 ТЗ).
func TestGenerateUseCase_AI_SignatureError_Explicit(t *testing.T) {
	billingClient := billing.NewMockClient(1.0) // полный доступ — тест проверяет ошибку AI
	quoteCase := NewQuoteUseCase(billingClient)
	generateCase := NewGenerateUseCase(billingClient, barcodegen.NewMockClient(), events.NewMockPublisher(), quoteCase).
		WithAI(&mockAIClient{sigErr: errors.New("ai service down")})

	_, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 1, Confirmed: true, BuildID: "b1", BatchID: "ba1",
		GenerateSignature: true,
		Fields:            map[string]any{"firstName": "JOHN", "signatureUrl": "existing"},
	})
	appErr := assertAppError(t, err)
	if appErr.HTTPStatus != 503 {
		t.Errorf("expected HTTP 503, got %d", appErr.HTTPStatus)
	}
}

// TestGenerateUseCase_AI_PhotoError_Explicit — AI упал при явном generatePhoto=true → 503 (п.9.3 ТЗ).
func TestGenerateUseCase_AI_PhotoError_Explicit(t *testing.T) {
	billingClient := billing.NewMockClient(1.0) // полный доступ — тест проверяет ошибку AI
	quoteCase := NewQuoteUseCase(billingClient)
	generateCase := NewGenerateUseCase(billingClient, barcodegen.NewMockClient(), events.NewMockPublisher(), quoteCase).
		WithAI(&mockAIClient{photoErr: errors.New("ai photo down")})

	_, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 1, Confirmed: true, BuildID: "b1", BatchID: "ba1",
		GeneratePhoto: true,
		Fields:        map[string]any{"firstName": "JOHN", "signatureUrl": "existing"},
	})
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T: %v", err, err)
	}
	if appErr.HTTPStatus != 503 {
		t.Errorf("expected HTTP 503, got %d", appErr.HTTPStatus)
	}
}

// TestGenerateUseCase_AI_AutoTrigger_ErrorIgnored — авто-триггер signatureUrl отсутствует,
// AI упал — ошибка игнорируется, генерация продолжается (п.9.3 ТЗ).
func TestGenerateUseCase_AI_AutoTrigger_ErrorIgnored(t *testing.T) {
	billingClient := billing.NewMockClient(1.0) // полный доступ — тест проверяет resilience AI
	quoteCase := NewQuoteUseCase(billingClient)
	generateCase := NewGenerateUseCase(billingClient, barcodegen.NewMockClient(), events.NewMockPublisher(), quoteCase).
		WithAI(&mockAIClient{sigErr: errors.New("ai down")})

	// generateSignature=false, signatureUrl не передан → авто-триггер, но ошибка не критична
	result, err := generateCase.Execute(context.Background(), "u-1", domain.GenerateRequest{
		Revision: "US_CA_08292017", BarcodeType: "pdf417",
		Units: 1, Confirmed: true, BuildID: "b1", BatchID: "ba1",
		GenerateSignature: false,
		Fields:            map[string]any{"firstName": "JOHN"},
	})
	if err != nil {
		t.Fatalf("auto-trigger AI error must be ignored, got: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true despite AI auto-trigger failure")
	}
}
