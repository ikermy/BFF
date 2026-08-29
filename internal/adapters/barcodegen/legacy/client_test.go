package legacy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ikermy/BFF/internal/adapters/barcodegen"
	"github.com/ikermy/BFF/internal/adapters/idempotency"
	"github.com/ikermy/BFF/internal/domain"
)

const testSecret = "test-access-secret"

func testClient(t *testing.T, baseURL, rawURL string) *LegacyClient {
	t.Helper()
	return NewLegacyClient(baseURL, testSecret, rawURL, 5*time.Second)
}

// captureServer — фейковый BarcodeGen, записывающий последний запрос и отвечающий кастомно.
type captureServer struct {
	lastPath    string
	lastHeaders http.Header
	lastBody    map[string]any
	respond     func(w http.ResponseWriter, r *http.Request)
}

func (s *captureServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.lastPath = r.URL.Path
		s.lastHeaders = r.Header
		_ = json.NewDecoder(r.Body).Decode(&s.lastBody)
		if s.respond != nil {
			s.respond(w, r)
			return
		}
		// Дефолтный ответ — валидная Prisma-запись Barcode.
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://cdn/default.png", "data": "ANSI\\x1e"})
	}
}

func TestGeneratePDF417_MapsAndInjectsRevision(t *testing.T) {
	srv := &captureServer{respond: func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "b1", "url": "https://cdn/x.png", "type": "PDF417",
			"data": "ANSI\\x1e\\r", "userId": "u1",
		})
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	c := testClient(t, ts.URL, "")
	ctx := barcodegen.WithUserID(context.Background(), "u-42")
	req := domain.GeneratePDF417Request{
		Revision: "US_CA_08292017",
		Fields:   map[string]any{"firstName": "John", "lastName": "Doe", "street": "123 Main"},
	}
	resp, err := c.GeneratePDF417(ctx, req)
	if err != nil {
		t.Fatalf("GeneratePDF417: %v", err)
	}

	if srv.lastPath != "/api/v1/barcodes/pdf417" {
		t.Errorf("path = %s, want /api/v1/barcodes/pdf417", srv.lastPath)
	}
	// Auth-заголовок с сервисным JWT, несущим id=u-42.
	auth := srv.lastHeaders.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		t.Fatalf("missing Authorization header, got %q", auth)
	}
	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(strings.TrimPrefix(auth, "Bearer "), claims, func(*jwt.Token) (any, error) { return []byte(testSecret), nil })
	if err != nil {
		t.Fatalf("parse minted JWT: %v", err)
	}
	if claims["id"] != "u-42" {
		t.Errorf("JWT id = %v, want u-42", claims["id"])
	}

	// Тело: values должны содержать AAMVA-поля + вброшенные DAJ/DDB из ревизии.
	values, _ := srv.lastBody["values"].(map[string]any)
	for k, want := range map[string]any{"DAC": "John", "DCS": "Doe", "DAG": "123 Main", "DAJ": "CA", "DDB": "08292017"} {
		if values[k] != want {
			t.Errorf("values[%s] = %v, want %v", k, values[k], want)
		}
	}

	if !resp.Success || resp.BarcodeURL != "https://cdn/x.png" || resp.Format != "PDF417" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if resp.Metadata.DataLength != len("ANSI\\x1e\\r") {
		t.Errorf("DataLength = %d", resp.Metadata.DataLength)
	}
}

func TestGeneratePDF417_UnknownFieldDropped(t *testing.T) {
	srv := &captureServer{respond: func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://cdn/x.png"})
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	c := testClient(t, ts.URL, "")

	_, err := c.GeneratePDF417(context.Background(), domain.GeneratePDF417Request{
		Revision: "US_CA_08292017",
		Fields:   map[string]any{"firstName": "John", "unknown": "x"},
	})
	if err != nil {
		t.Fatalf("GeneratePDF417: %v", err)
	}
	values, _ := srv.lastBody["values"].(map[string]any)
	if _, ok := values["unknown"]; ok {
		t.Error("unknown field should be dropped (forbidNonWhitelisted)")
	}
}

func TestGenerateCode128_LengthValidation(t *testing.T) {
	ts := httptest.NewServer((&captureServer{}).handler())
	defer ts.Close()
	c := testClient(t, ts.URL, "")

	if _, err := c.GenerateCode128(context.Background(), domain.GenerateCode128Request{Data: strings.Repeat("x", 26)}); err == nil {
		t.Error("expected error for data > 25 chars")
	}
	if _, err := c.GenerateCode128(context.Background(), domain.GenerateCode128Request{Data: "12345"}); err != nil {
		t.Errorf("unexpected error for valid data: %v", err)
	}
}

func TestCalculate_ReverseMapsValue(t *testing.T) {
	srv := &captureServer{respond: func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"Expiration Date:": "08/29/2027"})
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	c := testClient(t, ts.URL, "")

	val, err := c.Calculate(context.Background(), "US_CA_08292017", "DBA", map[string]any{"dateOfBirth": "08292017"})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if val != "08/29/2027" {
		t.Errorf("Calculate value = %v, want 08/29/2027", val)
	}
	if srv.lastPath != "/api/v1/barcodes/calculate" {
		t.Errorf("path = %s, want /api/v1/barcodes/calculate", srv.lastPath)
	}
	output, _ := srv.lastBody["output"].([]any)
	if len(output) != 1 || output[0] != "DBA" {
		t.Errorf("output = %v, want [DBA]", output)
	}
}

func TestCalculate_UnsupportedField(t *testing.T) {
	c := testClient(t, "http://unused", "")
	if _, err := c.Calculate(context.Background(), "US_CA_08292017", "DAQ", nil); err == nil {
		t.Error("expected FIELD_SOURCE_UNSUPPORTED for calculate DAQ")
	}
}

func TestRandom_ReturnsRequestedField(t *testing.T) {
	srv := &captureServer{respond: func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"Street:": "456 Oak Ave", "City:": "SF", "ZIP Code:": "94107"})
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	c := testClient(t, ts.URL, "")

	val, err := c.Random(context.Background(), "US_CA_08292017", "DAI", nil)
	if err != nil {
		t.Fatalf("Random: %v", err)
	}
	if val != "SF" {
		t.Errorf("Random value = %v, want SF", val)
	}
	output, _ := srv.lastBody["output"].([]any)
	want := []string{"DAG", "DAI", "DAK"}
	if len(output) != len(want) {
		t.Fatalf("output = %v, want %v", output, want)
	}
	for i, v := range output {
		if v != want[i] {
			t.Errorf("output[%d] = %v, want %v", i, v, want[i])
		}
	}
}

func TestRandom_UnsupportedField(t *testing.T) {
	c := testClient(t, "http://unused", "")
	if _, err := c.Random(context.Background(), "US_CA_08292017", "DAE", nil); err == nil {
		t.Error("expected FIELD_SOURCE_UNSUPPORTED for random DAE")
	}
}

func TestGenerateRaw_GoesToRawService(t *testing.T) {
	rawCalled := false
	rawSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawCalled = true
		if r.URL.Path != "/generate/raw" {
			t.Errorf("raw path = %s, want /generate/raw", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"imageUrl": "https://cdn/raw.png"})
	}))
	defer rawSrv.Close()

	bgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("GenerateRaw must not hit BarcodeGen")
	}))
	defer bgSrv.Close()

	c := testClient(t, bgSrv.URL, rawSrv.URL)
	resp, err := c.GenerateRaw(context.Background(), domain.GenerateRawRequest{NormalizedRaw: "ANSI...", Format: "pdf417"})
	if err != nil {
		t.Fatalf("GenerateRaw: %v", err)
	}
	if !rawCalled || resp.ImageUrl != "https://cdn/raw.png" {
		t.Errorf("rawCalled=%v resp=%+v", rawCalled, resp)
	}
}

func TestGenerateRaw_NoRawURL(t *testing.T) {
	c := testClient(t, "http://unused", "")
	if _, err := c.GenerateRaw(context.Background(), domain.GenerateRawRequest{}); err == nil {
		t.Error("expected error when BARCODEGEN_RAW_URL not configured")
	}
}

// ─── A6: идемпотентность генераций ──────────────────────────────────────────

func TestIdempotentGeneratePDF417_RetryReturnsCache(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://cdn/idem.png", "data": "ANSI"})
	}))
	defer ts.Close()

	c := NewLegacyClient(ts.URL, testSecret, "", 5*time.Second).
		WithIdempotencyStore(idempotency.NewMemoryStore(time.Hour))

	ctx := context.Background()
	req := domain.GeneratePDF417Request{IdempotencyKey: "k1", Revision: "US_CA_08292017", Fields: map[string]any{"firstName": "John"}}

	// Первый вызов → реальный запрос.
	first, err := c.GeneratePDF417(ctx, req)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// Повторный вызов с тем же ключом → ответ из кэша, BarcodeGen НЕ вызывается.
	second, err := c.GeneratePDF417(ctx, req)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected 1 call to BarcodeGen, got %d", got)
	}
	if first.BarcodeURL != second.BarcodeURL || first.BarcodeURL == "" {
		t.Errorf("cached response mismatch: first=%q second=%q", first.BarcodeURL, second.BarcodeURL)
	}
}

func TestIdempotentGeneratePDF417_ErrorClearsMarker(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError) // первый ретрай — 500
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://cdn/ok.png", "data": "ANSI"})
	}))
	defer ts.Close()

	c := NewLegacyClient(ts.URL, testSecret, "", 5*time.Second).
		WithIdempotencyStore(idempotency.NewMemoryStore(time.Hour))

	req := domain.GeneratePDF417Request{IdempotencyKey: "k-retry", Revision: "US_CA_08292017", Fields: map[string]any{"firstName": "John"}}

	// Первый вызов падает (500) — маркер должен быть снят, ключ освобождён.
	if _, err := c.GeneratePDF417(context.Background(), req); err == nil {
		t.Fatal("expected error on first (500)")
	}
	// Ретрай с тем же ключом должен снова реально вызвать BarcodeGen (не вернуть кэш).
	if _, err := c.GeneratePDF417(context.Background(), req); err != nil {
		t.Fatalf("retry after error: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected 2 calls (retry must hit BarcodeGen), got %d", got)
	}
}

func TestIdempotentGeneratePDF417_NoKeySkipsStore(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://cdn/nokey.png"})
	}))
	defer ts.Close()

	c := NewLegacyClient(ts.URL, testSecret, "", 5*time.Second).
		WithIdempotencyStore(idempotency.NewMemoryStore(time.Hour))

	req := domain.GeneratePDF417Request{Revision: "US_CA_08292017", Fields: map[string]any{"firstName": "John"}}
	for i := 0; i < 3; i++ {
		if _, err := c.GeneratePDF417(context.Background(), req); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("expected 3 calls when no idempotency key, got %d", got)
	}
}
