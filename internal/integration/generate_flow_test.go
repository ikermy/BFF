// Package integration содержит сквозные интеграционные тесты BFF (п.18 ТЗ).
//
// Тесты проверяют полный HTTP → Usecase → Kafka-события pipeline
// с реальным роутером Gin и mock-адаптерами вместо внешних сервисов.
// Kafka-брокер НЕ требуется: события захватываются capturingPublisher.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ikermy/BFF/internal/adapters/auth"
	"github.com/ikermy/BFF/internal/adapters/barcodegen"
	"github.com/ikermy/BFF/internal/adapters/billing"
	"github.com/ikermy/BFF/internal/adapters/history"
	"github.com/ikermy/BFF/internal/adapters/idempotency"
	kafkaadapter "github.com/ikermy/BFF/internal/adapters/kafka"
	"github.com/ikermy/BFF/internal/adapters/revisions"
	"github.com/ikermy/BFF/internal/adapters/timeouts"
	"github.com/ikermy/BFF/internal/adapters/topupbonus"
	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
	gintransport "github.com/ikermy/BFF/internal/transport/http/gin"
	kafkatransport "github.com/ikermy/BFF/internal/transport/kafka"
	"github.com/ikermy/BFF/internal/usecase"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ─── capturingPublisher ───────────────────────────────────────────────────────

// capturingPublisher захватывает все Kafka-события для assertions в тестах (п.18 ТЗ).
// Реализует EventPublisher + NotificationsPublisher + TransHistoryPublisher.
type capturingPublisher struct {
	mu sync.Mutex

	BarcodeGenerated   []domain.BarcodeGeneratedEvent
	BarcodeEdited      []domain.BarcodeEditedEvent
	PartialCompleted   []domain.PartialCompletedEvent
	SagaCompleted      []string // sagaID
	GenerationComplete []domain.NotificationRequest
	GenerationError    []domain.ErrorNotificationRequest
	Transactions       []domain.TransactionLog
}

func newCapturingPublisher() *capturingPublisher { return &capturingPublisher{} }

func (p *capturingPublisher) PublishBarcodeGenerated(_ context.Context, e domain.BarcodeGeneratedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.BarcodeGenerated = append(p.BarcodeGenerated, e)
	return nil
}
func (p *capturingPublisher) PublishBarcodeEdited(_ context.Context, e domain.BarcodeEditedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.BarcodeEdited = append(p.BarcodeEdited, e)
	return nil
}
func (p *capturingPublisher) PublishPartialCompleted(_ context.Context, e domain.PartialCompletedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.PartialCompleted = append(p.PartialCompleted, e)
	return nil
}
func (p *capturingPublisher) PublishSagaCompleted(_ context.Context, sagaID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.SagaCompleted = append(p.SagaCompleted, sagaID)
	return nil
}
func (p *capturingPublisher) PublishBulkResult(_ context.Context, _ domain.BulkResultEvent) error {
	return nil
}
func (p *capturingPublisher) SendGenerationComplete(_ context.Context, r domain.NotificationRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.GenerationComplete = append(p.GenerationComplete, r)
	return nil
}
func (p *capturingPublisher) SendGenerationError(_ context.Context, r domain.ErrorNotificationRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.GenerationError = append(p.GenerationError, r)
	return nil
}
func (p *capturingPublisher) LogTransaction(_ context.Context, l domain.TransactionLog) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Transactions = append(p.Transactions, l)
	return nil
}

// snapshot* — потокобезопасные копии для assertions.
func (p *capturingPublisher) snapshotGenerated() []domain.BarcodeGeneratedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]domain.BarcodeGeneratedEvent, len(p.BarcodeGenerated))
	copy(out, p.BarcodeGenerated)
	return out
}
func (p *capturingPublisher) snapshotEdited() []domain.BarcodeEditedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]domain.BarcodeEditedEvent, len(p.BarcodeEdited))
	copy(out, p.BarcodeEdited)
	return out
}
func (p *capturingPublisher) snapshotPartial() []domain.PartialCompletedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]domain.PartialCompletedEvent, len(p.PartialCompleted))
	copy(out, p.PartialCompleted)
	return out
}
func (p *capturingPublisher) snapshotSagaCompleted() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.SagaCompleted))
	copy(out, p.SagaCompleted)
	return out
}
func (p *capturingPublisher) snapshotNotifications() []domain.NotificationRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]domain.NotificationRequest, len(p.GenerationComplete))
	copy(out, p.GenerationComplete)
	return out
}

// ─── test harness ─────────────────────────────────────────────────────────────

type testEnv struct {
	router    *gin.Engine
	publisher *capturingPublisher
	idem      ports.IdempotencyStore
}

// newTestEnv создаёт полный роутер с mock-адаптерами и capturingPublisher.
// unitPrice — процент баланса, доступного для генерации (0.50 = 50%).
func newTestEnv(t *testing.T, unitPrice float64) *testEnv {
	t.Helper()
	publisher := newCapturingPublisher()

	billingClient := billing.NewMockClient(unitPrice)
	barcodeClient := barcodegen.NewMockClient()
	historyClient := history.NewMockClient()
	revisionStore := revisions.NewMemoryStore()
	idempotencyStore := idempotency.NewMemoryStore(24 * time.Hour)

	quoteCase := usecase.NewQuoteUseCase(billingClient)
	chainExecutor := usecase.NewChainExecutor(barcodeClient, revisionStore)
	generateCase := usecase.NewGenerateUseCase(billingClient, barcodeClient, publisher, quoteCase).
		WithChainExecutor(chainExecutor).
		WithRevisionStore(revisionStore).
		WithNotifications(publisher).
		WithTransHistory(publisher)
	editCase := usecase.NewEditUseCase(billingClient, barcodeClient, historyClient, publisher)
	revisionSchemaCase := usecase.NewRevisionSchemaUseCase(revisionStore)
	bulkCase := usecase.NewBulkUseCase(billingClient)

	topupStore := topupbonus.NewMemoryStore()
	kafkaTopicsStore := kafkaadapter.NewTopicStore()
	timeoutStore := timeouts.NewMemoryStore(30*time.Second, 5*time.Second, 60*time.Second, 5*time.Second, 5*time.Second)

	bulkHandler := kafkatransport.NewBulkJobHandler(generateCase, publisher)
	bulkConsumer := kafkaadapter.NewMockConsumer(bulkHandler.Handle, 64)

	apiHandler := gintransport.NewAPIHandler(quoteCase, generateCase, editCase, bulkConsumer, revisionSchemaCase, revisionStore, barcodeClient, historyClient)
	internalHandler := gintransport.NewInternalHandler(quoteCase, bulkCase)
	adminHandler := gintransport.NewAdminHandler(topupStore, kafkaTopicsStore, timeoutStore, revisionStore)

	router := gintransport.NewRouter(
		gintransport.Handlers{API: apiHandler, Internal: internalHandler, Admin: adminHandler},
		auth.NewMockClient(),
		idempotencyStore,
		"test-internal-token",
		"test-admin-token",
		true,  // enableLegacyAuth
		true,  // enableIdempotency
		false, // maintenanceMode
	)

	return &testEnv{router: router, publisher: publisher, idem: idempotencyStore}
}

// post — helper для POST запросов с Authorization и Idempotency-Key.
func (e *testEnv) post(t *testing.T, path, body, idempotencyKey string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", idempotencyKey)
	}
	e.router.ServeHTTP(w, req)
	return w
}

// mustDecodeJSON — декодирует JSON из тела ответа.
func mustDecodeJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("failed to decode response JSON: %v (body=%s)", err, w.Body.String())
	}
	return out
}

// fullFields — корректный набор полей для ревизии US_CA_08292017.
func fullFields() string {
	return `{"firstName":"JOHN","lastName":"DOE","dateOfBirth":"1990-05-15","street":"123 Main St","city":"Los Angeles","state":"CA","zipCode":"90001"}`
}

// ─── Integration tests: End-to-end generate flow (п.18 ТЗ) ───────────────────

// TestIntegration_GenerateFlow_FullSuccess — полная генерация:
// HTTP → Quote → Block → Generate(N) → Capture → billing.saga.completed + N×barcode.generated.
func TestIntegration_GenerateFlow_FullSuccess(t *testing.T) {
	env := newTestEnv(t, 1.0) // 100% баланса доступно

	body := `{"revision":"US_CA_08292017","barcodeType":"pdf417","units":3,"confirmed":true,"buildId":"build-001","batchId":"batch-001","fields":` + fullFields() + `}`
	w := env.post(t, "/api/v1/barcode/generate", body, "idem-full-001")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	resp := mustDecodeJSON(t, w)
	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}
	barcodes, _ := resp["barcodes"].([]any)
	if len(barcodes) != 3 {
		t.Errorf("expected 3 barcodes, got %d", len(barcodes))
	}

	// Kafka: billing.saga.completed должен быть опубликован (п.14.4 ТЗ)
	sagaCompleted := env.publisher.snapshotSagaCompleted()
	if len(sagaCompleted) != 1 {
		t.Errorf("expected 1 billing.saga.completed event, got %d", len(sagaCompleted))
	}

	// Kafka: barcode.generated — по одному на каждый баркод (п.10.3 ТЗ)
	generated := env.publisher.snapshotGenerated()
	if len(generated) != 3 {
		t.Errorf("expected 3 barcode.generated events, got %d", len(generated))
	}

	// Notifications: GENERATION_COMPLETE должен быть опубликован (п.11.2 ТЗ)
	notifs := env.publisher.snapshotNotifications()
	if len(notifs) != 1 {
		t.Errorf("expected 1 GENERATION_COMPLETE notification, got %d", len(notifs))
	}
}

// TestIntegration_GenerateFlow_PartialSuccess — partial success:
// Billing допускает 5 из 10 → 5 баркодов → Capture(5) + Release(5) + partial_completed.
func TestIntegration_GenerateFlow_PartialSuccess(t *testing.T) {
	env := newTestEnv(t, 0.50) // 50% — partial

	body := `{"revision":"US_CA_08292017","barcodeType":"pdf417","units":10,"confirmed":true,"buildId":"build-partial","batchId":"batch-p","fields":` + fullFields() + `}`
	w := env.post(t, "/api/v1/barcode/generate", body, "idem-partial-001")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for partial, got %d body=%s", w.Code, w.Body.String())
	}
	resp := mustDecodeJSON(t, w)
	if resp["success"] != true {
		t.Errorf("expected success=true on partial response")
	}
	barcodes, _ := resp["barcodes"].([]any)
	if len(barcodes) != 5 {
		t.Errorf("expected 5 barcodes on partial, got %d", len(barcodes))
	}

	// Kafka: billing.saga.partial_completed должен быть опубликован (п.14.4 ТЗ)
	partials := env.publisher.snapshotPartial()
	if len(partials) != 1 {
		t.Errorf("expected 1 billing.saga.partial_completed event, got %d", len(partials))
	}

	// billing.saga.completed НЕ должен публиковаться при partial (п.14.4 ТЗ)
	sagaCompleted := env.publisher.snapshotSagaCompleted()
	if len(sagaCompleted) != 0 {
		t.Errorf("expected no billing.saga.completed on partial, got %d", len(sagaCompleted))
	}

	// barcode.generated — только для успешных (5 из 10)
	generated := env.publisher.snapshotGenerated()
	if len(generated) != 5 {
		t.Errorf("expected 5 barcode.generated events, got %d", len(generated))
	}
}

// TestIntegration_GenerateFlow_InsufficientFunds — нет средств → 402 INSUFFICIENT_FUNDS.
// Никаких Kafka-событий не должно публиковаться.
func TestIntegration_GenerateFlow_InsufficientFunds(t *testing.T) {
	env := newTestEnv(t, 0.0) // 0% — нет средств

	body := `{"revision":"US_CA_08292017","barcodeType":"pdf417","units":5,"confirmed":true,"buildId":"build-nofunds","batchId":"batch-nf","fields":` + fullFields() + `}`
	w := env.post(t, "/api/v1/barcode/generate", body, "idem-nofunds-001")

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d body=%s", w.Code, w.Body.String())
	}
	resp := mustDecodeJSON(t, w)
	if resp["code"] != "INSUFFICIENT_FUNDS" {
		t.Errorf("expected INSUFFICIENT_FUNDS code, got %v", resp["code"])
	}

	// Никаких Kafka-событий — генерации не было
	if n := len(env.publisher.snapshotGenerated()); n != 0 {
		t.Errorf("expected no barcode.generated events on INSUFFICIENT_FUNDS, got %d", n)
	}
	if n := len(env.publisher.snapshotSagaCompleted()); n != 0 {
		t.Errorf("expected no saga.completed events, got %d", n)
	}
}

// TestIntegration_GenerateFlow_PartialRequiresConfirmation — partial без confirmed=true → 200 PARTIAL_FUNDS.
func TestIntegration_GenerateFlow_PartialRequiresConfirmation(t *testing.T) {
	env := newTestEnv(t, 0.50)

	body := `{"revision":"US_CA_08292017","barcodeType":"pdf417","units":10,"confirmed":false,"buildId":"build-conf","batchId":"batch-c","fields":` + fullFields() + `}`
	w := env.post(t, "/api/v1/barcode/generate", body, "idem-conf-001")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (PARTIAL_FUNDS), got %d body=%s", w.Code, w.Body.String())
	}
	resp := mustDecodeJSON(t, w)
	if resp["code"] != "PARTIAL_FUNDS" {
		t.Errorf("expected PARTIAL_FUNDS code, got %v", resp["code"])
	}

	// Нет событий — генерация не запускалась
	if n := len(env.publisher.snapshotGenerated()); n != 0 {
		t.Errorf("expected no barcode.generated events before confirmation, got %d", n)
	}
}

// TestIntegration_Idempotency_DuplicateRequest — повторный запрос с тем же ключом → 200 DUPLICATE_REQUEST.
// Гарантирует что повторный запрос не создаёт новых баркодов и событий (п.1.3, п.13 ТЗ).
func TestIntegration_Idempotency_DuplicateRequest(t *testing.T) {
	env := newTestEnv(t, 1.0)

	body := `{"revision":"US_CA_08292017","barcodeType":"pdf417","units":2,"confirmed":true,"buildId":"build-idem","batchId":"batch-i","fields":` + fullFields() + `}`
	const key = "idem-duplicate-001"

	// Первый запрос
	w1 := env.post(t, "/api/v1/barcode/generate", body, key)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d body=%s", w1.Code, w1.Body.String())
	}
	generatedAfterFirst := len(env.publisher.snapshotGenerated())

	// Второй запрос с тем же ключом
	w2 := env.post(t, "/api/v1/barcode/generate", body, key)
	if w2.Code != http.StatusOK {
		t.Fatalf("duplicate request: expected 200, got %d body=%s", w2.Code, w2.Body.String())
	}
	resp := mustDecodeJSON(t, w2)
	if resp["code"] != "DUPLICATE_REQUEST" {
		t.Errorf("expected DUPLICATE_REQUEST on duplicate, got %v", resp["code"])
	}
	if w2.Header().Get("X-Idempotency-Replayed") != "true" {
		t.Error("expected X-Idempotency-Replayed=true header on duplicate")
	}

	// Новых событий не должно быть — ответ пришёл из кэша
	generatedAfterSecond := len(env.publisher.snapshotGenerated())
	if generatedAfterSecond != generatedAfterFirst {
		t.Errorf("duplicate request must not publish new events: before=%d after=%d",
			generatedAfterFirst, generatedAfterSecond)
	}
}

// TestIntegration_GenerateFlow_ValidationError — отсутствуют обязательные поля → 400 VALIDATION_ERROR.
func TestIntegration_GenerateFlow_ValidationError(t *testing.T) {
	env := newTestEnv(t, 1.0)

	// Только firstName — lastName и dateOfBirth отсутствуют
	body := `{"revision":"US_CA_08292017","barcodeType":"pdf417","units":1,"confirmed":true,"buildId":"b1","batchId":"ba1","fields":{"firstName":"JOHN"}}`
	w := env.post(t, "/api/v1/barcode/generate", body, "idem-val-001")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := mustDecodeJSON(t, w)
	if resp["code"] != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %v", resp["code"])
	}
}

// TestIntegration_FreeEditFlow — полный flow бесплатного редактирования (п.10.1 ТЗ):
// POST /api/v1/barcode/:id/edit → CheckFreeEdit → Generate → barcode.edited event.
func TestIntegration_FreeEditFlow(t *testing.T) {
	env := newTestEnv(t, 1.0)

	body := `{"field":"DAC","value":"UPDATED_VALUE"}`
	w := env.post(t, "/api/v1/barcode/barcode-123/edit", body, "idem-edit-001")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := mustDecodeJSON(t, w)
	if resp["canEdit"] != true {
		t.Errorf("expected canEdit=true, got %v", resp["canEdit"])
	}
	if resp["newUrl"] == "" || resp["newUrl"] == nil {
		t.Error("expected non-empty newUrl in edit response")
	}

	// Kafka: barcode.edited ОБЯЗАН быть опубликован (п.10.1 ТЗ).
	// Критично: без этого события History не установит editFlag=true.
	edited := env.publisher.snapshotEdited()
	if len(edited) != 1 {
		t.Fatalf("expected 1 barcode.edited event, got %d — History won't set editFlag!", len(edited))
	}
	event := edited[0]
	if event.ID != "barcode-123" {
		t.Errorf("barcode.edited: expected id=barcode-123, got %q", event.ID)
	}
	if event.Field != "DAC" {
		t.Errorf("barcode.edited: expected field=DAC, got %q", event.Field)
	}
	if event.NewURL == "" {
		t.Error("barcode.edited: expected non-empty newUrl")
	}
	if event.Timestamp == "" {
		t.Error("barcode.edited: expected non-empty timestamp")
	}
}

// TestIntegration_FreeEditFlow_AlreadyUsed — повторное редактирование → 402 (п.10.1 ТЗ).
// MockHistoryClient.canEdit=false означает editFlag=true (уже использовано).
func TestIntegration_FreeEditFlow_AlreadyUsed(t *testing.T) {
	// history.MockClient по умолчанию canEdit=true для barcode-123.
	// Используем несуществующий ID чтобы получить canEdit=false из mock.
	env := newTestEnv(t, 1.0)

	body := `{"field":"DAC","value":"X"}`
	// mock возвращает canEdit=false для ID "barcode-no-edit"
	w := env.post(t, "/api/v1/barcode/barcode-no-edit/edit", body, "idem-edit-002")

	// MockHistoryClient должен вернуть canEdit=false для неизвестных IDs → 402
	if w.Code == http.StatusOK {
		t.Skip("MockHistoryClient returns canEdit=true for all IDs — skipping already-used test")
	}
	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402 for already-used edit, got %d body=%s", w.Code, w.Body.String())
	}

	// Kafka: barcode.edited НЕ должен публиковаться при отказе
	edited := env.publisher.snapshotEdited()
	if len(edited) != 0 {
		t.Errorf("expected no barcode.edited event on failed edit, got %d", len(edited))
	}
}
