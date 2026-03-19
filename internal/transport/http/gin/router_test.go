package gintransport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikermy/BFF/internal/adapters/auth"
	"github.com/ikermy/BFF/internal/adapters/barcodegen"
	"github.com/ikermy/BFF/internal/adapters/billing"
	"github.com/ikermy/BFF/internal/adapters/events"
	"github.com/ikermy/BFF/internal/adapters/history"
	"github.com/ikermy/BFF/internal/adapters/idempotency"
	kafkaadapter "github.com/ikermy/BFF/internal/adapters/kafka"
	"github.com/ikermy/BFF/internal/adapters/revisions"
	"github.com/ikermy/BFF/internal/adapters/timeouts"
	"github.com/ikermy/BFF/internal/adapters/topupbonus"
	"github.com/ikermy/BFF/internal/ports"
	gintransport "github.com/ikermy/BFF/internal/transport/http/gin"
	kafkatransport "github.com/ikermy/BFF/internal/transport/kafka"
	"github.com/ikermy/BFF/internal/usecase"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func mustNewRequest(t *testing.T, method, target string, body io.Reader) *http.Request {
	t.Helper()
	return httptest.NewRequest(method, target, body)
}

func mustDecodeJSON(t *testing.T, body io.Reader, out any) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(out); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}
}

// makeHandlers собирает общие компоненты и возвращает готовые handlers и idempotency store.
// Если topupPath != "" и t != nil — загрузит topup config из файла (используется в тестах на персист).
func makeHandlers(t *testing.T, topupPath string) (gintransport.Handlers, ports.IdempotencyStore) {
	if t != nil {
		t.Helper()
	}

	billingClient := billing.NewMockClient(1.0)
	barcodeClient := barcodegen.NewMockClient()
	eventPublisher := events.NewMockPublisher()
	historyClient := history.NewMockClient()
	revisionStore := revisions.NewMemoryStore()

	quoteCase := usecase.NewQuoteUseCase(billingClient)
	bulkCase := usecase.NewBulkUseCase(billingClient)
	chainExecutor := usecase.NewChainExecutor(barcodeClient, revisionStore)
	generateCase := usecase.NewGenerateUseCase(billingClient, barcodeClient, eventPublisher, quoteCase).
		WithChainExecutor(chainExecutor).
		WithRevisionStore(revisionStore)
	editCase := usecase.NewEditUseCase(billingClient, barcodeClient, historyClient, eventPublisher)
	revisionSchemaCase := usecase.NewRevisionSchemaUseCase(revisionStore)

	topupStore := topupbonus.NewMemoryStore()
	if topupPath != "" {
		if t == nil {
			panic("topupPath provided but testing.T is nil")
		}
		if err := topupStore.LoadFromFile(topupPath); err != nil {
			t.Fatalf("LoadFromFile(topup) returned error: %v", err)
		}
	}
	kafkaTopicsStore := kafkaadapter.NewTopicStore()
	timeoutStore := timeouts.NewMemoryStore(30*time.Second, 5*time.Second, 60*time.Second, 5*time.Second, 5*time.Second)
	idempotencyStore := idempotency.NewMemoryStore(24 * time.Hour)

	bulkHandler := kafkatransport.NewBulkJobHandler(generateCase, eventPublisher)
	bulkConsumer := kafkaadapter.NewMockConsumer(bulkHandler.Handle, 64)

	apiHandler := gintransport.NewAPIHandler(quoteCase, generateCase, editCase, bulkConsumer, revisionSchemaCase, revisionStore, barcodeClient, historyClient)
	internalHandler := gintransport.NewInternalHandler(quoteCase, bulkCase)
	adminHandler := gintransport.NewAdminHandler(topupStore, kafkaTopicsStore, timeoutStore, revisionStore)

	return gintransport.Handlers{API: apiHandler, Internal: internalHandler, Admin: adminHandler}, idempotencyStore
}

// buildTestRouterWithFlags — оригинальная функциональность, теперь с делегированием в makeHandlers.
func buildTestRouterWithFlags(enableLegacyAuth, enableIdempotency, maintenanceMode bool) *gin.Engine {
	handlers, idempotencyStore := makeHandlers(nil, "")
	return gintransport.NewRouter(
		handlers,
		auth.NewMockClient(),
		idempotencyStore,
		"test-internal-token",
		"test-admin-token",
		enableLegacyAuth,
		enableIdempotency,
		maintenanceMode,
	)
}

func buildTestRouter() *gin.Engine {
	return buildTestRouterWithFlags(true, true, false)
}

func buildTestRouterWithTopupFile(t *testing.T, path string) *gin.Engine {
	handlers, idempotencyStore := makeHandlers(t, path)
	return gintransport.NewRouter(
		handlers,
		auth.NewMockClient(),
		idempotencyStore,
		"test-internal-token",
		"test-admin-token",
		true,
		true,
		false,
	)
}

func TestHealth(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestMetrics(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Fatal("expected non-empty metrics body")
	}
}

func TestMaintenanceMode_BlocksGenerateButAllowsHealth(t *testing.T) {
	r := buildTestRouterWithFlags(true, true, true)

	wHealth := httptest.NewRecorder()
	reqHealth := mustNewRequest(t, http.MethodGet, "/health", nil)
	r.ServeHTTP(wHealth, reqHealth)
	if wHealth.Code != http.StatusOK {
		t.Fatalf("health must stay available in maintenance, got %d", wHealth.Code)
	}

	w := httptest.NewRecorder()
	body := `{"revision":"US_CA_08292017","barcodeType":"pdf417","units":1,"confirmed":true,"buildId":"m1","batchId":"ba1","fields":{"firstName":"JOHN","lastName":"DOE","dateOfBirth":"1990-05-15","street":"123 Main St","city":"Los Angeles","state":"CA","zipCode":"90001"}}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/barcode/generate", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Idempotency-Key", "maintenance-gen-001")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 in maintenance mode, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestLegacyAuthDisabled_AllowsUserRouteWithoutAuthorization(t *testing.T) {
	r := buildTestRouterWithFlags(false, true, false)
	w := httptest.NewRecorder()
	req := mustNewRequest(t, http.MethodGet, "/api/v1/billing/quote?units=10&revision=US_CA_08292017", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with legacy auth disabled, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetQuote_Unauthorized(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/billing/quote?units=10", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGetQuote_Success(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	req := mustNewRequest(t, http.MethodGet, "/api/v1/billing/quote?units=10&revision=US_CA_08292017", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	mustDecodeJSON(t, w.Body, &resp)
	if resp["canProcess"] != true {
		t.Errorf("expected canProcess=true, got %v", resp)
	}
}

func TestGetQuote_InvalidUnits(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	req := mustNewRequest(t, http.MethodGet, "/api/v1/billing/quote?units=abc", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestListRevisions(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	req := mustNewRequest(t, http.MethodGet, "/api/v1/revisions", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	mustDecodeJSON(t, w.Body, &resp)
	items, _ := resp["revisions"].([]any)
	if len(items) == 0 {
		t.Errorf("expected non-empty revisions list")
	}
}

func TestGetRevisionSchema_Success(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	req := mustNewRequest(t, http.MethodGet, "/api/v1/revisions/US_CA_08292017/schema", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetRevisionSchema_NotFound(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	req := mustNewRequest(t, http.MethodGet, "/api/v1/revisions/UNKNOWN/schema", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestGenerate_MissingIdempotencyKey(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	body := `{"revision":"US_CA_08292017","barcodeType":"pdf417","units":1,"confirmed":true,"buildId":"b1","batchId":"ba1"}`
	req := mustNewRequest(t, http.MethodPost, "/api/v1/barcode/generate", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 without idempotency key, got %d", w.Code)
	}
}

func TestGenerate_Success(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	body := `{"revision":"US_CA_08292017","barcodeType":"pdf417","units":1,"confirmed":true,"buildId":"b1","batchId":"ba1","fields":{"firstName":"JOHN","lastName":"DOE","dateOfBirth":"1990-05-15","street":"123 Main St","city":"Los Angeles","state":"CA","zipCode":"90001"}}`
	req := mustNewRequest(t, http.MethodPost, "/api/v1/barcode/generate", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Idempotency-Key", "gen-test-001")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	mustDecodeJSON(t, w.Body, &resp)
	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp)
	}
}

func TestGenerate_IdempotencyReplay(t *testing.T) {
	r := buildTestRouter()
	body := `{"revision":"US_CA_08292017","barcodeType":"pdf417","units":1,"confirmed":true,"buildId":"b2","batchId":"ba2","fields":{"firstName":"JOHN","lastName":"DOE","dateOfBirth":"1990-05-15","street":"123 Main St","city":"Los Angeles","state":"CA","zipCode":"90001"}}`

	doReq := func() *httptest.ResponseRecorder {
		ww := httptest.NewRecorder()
		rr := mustNewRequest(t, http.MethodPost, "/api/v1/barcode/generate", bytes.NewBufferString(body))
		rr.Header.Set("Authorization", "Bearer valid-token")
		rr.Header.Set("Content-Type", "application/json")
		rr.Header.Set("X-Idempotency-Key", "replay-key-999")
		r.ServeHTTP(ww, rr)
		return ww
	}

	w1 := doReq()
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d body=%s", w1.Code, w1.Body.String())
	}
	w2 := doReq()
	if w2.Code != http.StatusOK {
		t.Fatalf("replay: expected 200, got %d", w2.Code)
	}
	if w2.Header().Get("X-Idempotency-Replayed") != "true" {
		t.Error("expected X-Idempotency-Replayed: true on replay")
	}
	var replayResp map[string]any
	mustDecodeJSON(t, w2.Body, &replayResp)
	if replayResp["code"] != "DUPLICATE_REQUEST" {
		t.Fatalf("expected code=DUPLICATE_REQUEST on replay, got %v", replayResp)
	}
	if _, exists := replayResp["duplicate"]; exists {
		t.Fatalf("did not expect extra duplicate field in replay response, got %v", replayResp)
	}
}

func TestGeneratePDF417_Success(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	body := `{"revision":"US_CA_08292017","fields":{"firstName":"JOHN"},"options":{"width":420,"height":160}}`
	req := mustNewRequest(t, http.MethodPost, "/api/v1/barcode/generate/pdf417", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Idempotency-Key", "pdf417-001")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["success"] != true {
		t.Fatalf("expected success=true, got %v", resp)
	}
	if resp["format"] != "pdf417" {
		t.Fatalf("expected format=pdf417, got %v", resp["format"])
	}
}

func TestGenerateCode128_Success(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	body := `{"data":"ABC-123","buildId":"c128-1","options":{"width":320,"height":110}}`
	req := mustNewRequest(t, http.MethodPost, "/api/v1/barcode/generate/code128", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Idempotency-Key", "code128-001")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["success"] != true {
		t.Fatalf("expected success=true, got %v", resp)
	}
	if resp["format"] != "code128" {
		t.Fatalf("expected format=code128, got %v", resp["format"])
	}
}

func TestGetBarcode_Success(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	req := mustNewRequest(t, http.MethodGet, "/api/v1/barcode/barcode-123", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["id"] != "barcode-123" {
		t.Fatalf("expected id=barcode-123, got %v", resp["id"])
	}
}

func TestEditBarcode_Success(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	body := `{"field":"DAC","value":"UPDATED_VALUE"}`
	req := mustNewRequest(t, http.MethodPost, "/api/v1/barcode/barcode-123/edit", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Idempotency-Key", "edit-test-001")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	mustDecodeJSON(t, w.Body, &resp)
	if resp["canEdit"] != true {
		t.Errorf("expected canEdit=true, got %v", resp)
	}
}

func TestEditBarcode_MissingIdempotencyKey(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	body := `{"field":"DAC","value":"UPDATED_VALUE"}`
	req := mustNewRequest(t, http.MethodPost, "/api/v1/barcode/barcode-123/edit", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without idempotency key, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAdminListRevisions_Forbidden(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	req := mustNewRequest(t, http.MethodGet, "/admin/revisions", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestAdminListRevisions_Success(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	req := mustNewRequest(t, http.MethodGet, "/admin/revisions", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAdminUpdateTimeouts_Success(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	body := `{"barcodeGen":30000,"billing":5000,"ai":60000}`
	req := mustNewRequest(t, http.MethodPut, "/admin/config/timeouts", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Idempotency-Key", "admin-timeouts-001")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAdminUpdateTimeouts_MissingIdempotencyKey(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	body := `{"barcodeGen":30000,"billing":5000,"ai":60000}`
	req := mustNewRequest(t, http.MethodPut, "/admin/config/timeouts", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without idempotency key, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAdminUpdateTimeouts_TooLarge(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	body := `{"barcodeGen":300001,"billing":5000,"ai":60000}`
	req := mustNewRequest(t, http.MethodPut, "/admin/config/timeouts", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Idempotency-Key", "admin-timeouts-too-large")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAdminUpdateRevision_InvalidSource(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	body := `{"enabled":true,"calculationChain":[{"field":"DAQ","source":"invalid"}]}`
	req := mustNewRequest(t, http.MethodPut, "/admin/revisions/US_CA_08292017", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Idempotency-Key", "admin-revision-invalid-source")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAdminUpdateTopupBonus_Overlap(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	body := `{"enabled":true,"tiers":[{"minAmount":0,"maxAmount":100,"bonusPercent":5},{"minAmount":50,"maxAmount":200,"bonusPercent":10}]}`
	req := mustNewRequest(t, http.MethodPut, "/admin/config/topup-bonus", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Idempotency-Key", "admin-topup-overlap")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAdminUpdateTopupBonus_PersistsToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "topup-bonus.yaml")
	r := buildTestRouterWithTopupFile(t, path)
	w := httptest.NewRecorder()
	body := `{"enabled":true,"tiers":[{"minAmount":10,"maxAmount":49.99,"bonusPercent":5},{"minAmount":50,"maxAmount":99.99,"bonusPercent":10},{"minAmount":100,"maxAmount":null,"bonusPercent":15}]}`
	req := mustNewRequest(t, http.MethodPut, "/admin/config/topup-bonus", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Idempotency-Key", "admin-topup-persist")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	reloaded := topupbonus.NewMemoryStore()
	if err := reloaded.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile(reloaded) returned error: %v", err)
	}
	cfg, err := reloaded.Get(context.Background())
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !cfg.Enabled {
		t.Fatalf("expected enabled=true, got %+v", cfg)
	}
	if len(cfg.Tiers) != 3 {
		t.Fatalf("expected 3 tiers, got %+v", cfg)
	}
	if cfg.Tiers[0].BonusPercent != 5 || cfg.Tiers[1].BonusPercent != 10 || cfg.Tiers[2].BonusPercent != 15 {
		t.Fatalf("unexpected persisted tiers: %+v", cfg.Tiers)
	}
	if cfg.Tiers[2].MaxAmount != nil {
		t.Fatalf("expected last tier maxAmount=nil, got %+v", cfg.Tiers[2].MaxAmount)
	}
}

func TestAdminKafkaTopics(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	req := mustNewRequest(t, http.MethodGet, "/admin/kafka/topics", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	mustDecodeJSON(t, w.Body, &resp)
	topics, _ := resp["topics"].([]any)
	if len(topics) == 0 {
		t.Errorf("expected non-empty topics list")
	}
}

func TestInternalValidate_Success(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	body := `{"userId":"u1","batchId":"b1","buildId":"bd1","rowNumber":1,"revision":"US_CA_08292017","fields":{"firstName":"JOHN"}}`
	req := mustNewRequest(t, http.MethodPost, "/internal/validate", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-internal-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	mustDecodeJSON(t, w.Body, &resp)
	if resp["valid"] != true {
		t.Errorf("expected valid=true, got %v", resp)
	}
}

func TestInternalQuote_Success(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	body := `{"userId":"u1","count":10,"revision":"US_CA_08292017"}`
	req := mustNewRequest(t, http.MethodPost, "/internal/billing/quote", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-internal-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["canProcess"] != true {
		t.Fatalf("expected canProcess=true, got %v", resp)
	}
}

func TestInternalQuote_InvalidJSON(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	req := mustNewRequest(t, http.MethodPost, "/internal/billing/quote", bytes.NewBufferString(`{"userId":`))
	req.Header.Set("Authorization", "Bearer test-internal-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestInternalBlockBatch_Success(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	body := `{"userId":"u1","count":2,"batchId":"batch-1"}`
	req := mustNewRequest(t, http.MethodPost, "/internal/billing/block-batch", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-internal-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Idempotency-Key", "internal-block-batch-001")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	txIDs, ok := resp["transactionIds"].([]any)
	if !ok || len(txIDs) != 2 {
		t.Fatalf("expected 2 transactionIds, got %v", resp)
	}
}

func TestBulkWake_Success(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	req := mustNewRequest(t, http.MethodPost, "/api/v1/bulk/wake", nil)
	req.Header.Set("Authorization", "Bearer test-internal-token")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "awake" {
		t.Fatalf("expected status=awake, got %v", resp)
	}
}

func TestInternalBlockBatch_MissingIdempotencyKey(t *testing.T) {
	r := buildTestRouter()
	w := httptest.NewRecorder()
	body := `{"userId":"u1","count":2,"batchId":"b1"}`
	req := mustNewRequest(t, http.MethodPost, "/internal/billing/block-batch", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-internal-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without idempotency key, got %d body=%s", w.Code, w.Body.String())
	}
}
