package barcodegen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ikermy/BFF/internal/adapters/timeouts"
	"github.com/ikermy/BFF/internal/domain"
)

func TestHTTPClient_GeneratePDF417ForwardsBodyAndIdempotencyKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/generate/pdf417" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Idempotency-Key"); got != "pdf417-key-1" {
			t.Fatalf("expected X-Idempotency-Key=pdf417-key-1, got %q", got)
		}

		var body pdf417ReqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Revision != "US_CA_08292017" || body.BuildID != "build-1" || body.BatchID != "batch-1" {
			t.Fatalf("unexpected request body: %+v", body)
		}
		if body.Fields["firstName"] != "JOHN" {
			t.Fatalf("unexpected fields: %+v", body.Fields)
		}
		if body.Options.Width != 420 || body.Options.Height != 160 {
			t.Fatalf("unexpected options: %+v", body.Options)
		}

		_ = json.NewEncoder(w).Encode(domain.GeneratePDF417Response{
			Success:    true,
			BarcodeURL: "https://cdn.example.com/pdf417.png",
			Format:     "pdf417",
			Metadata:   domain.BarcodeMetadata{Width: 420, Height: 160},
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second)
	resp, err := client.GeneratePDF417(context.Background(), domain.GeneratePDF417Request{
		Revision:       "US_CA_08292017",
		BuildID:        "build-1",
		BatchID:        "batch-1",
		Fields:         map[string]any{"firstName": "JOHN"},
		Options:        domain.BarcodeOptions{Width: 420, Height: 160},
		IdempotencyKey: "pdf417-key-1",
	})
	if err != nil {
		t.Fatalf("GeneratePDF417 returned error: %v", err)
	}
	if !resp.Success || resp.Format != "pdf417" || resp.BarcodeURL == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHTTPClient_GenerateCode128MapsDataIntoFieldsAndForwardsIdempotencyKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/generate/code128" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Idempotency-Key"); got != "code128-key-1" {
			t.Fatalf("expected X-Idempotency-Key=code128-key-1, got %q", got)
		}

		var body code128ReqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if data := body.Fields["data"]; data != "ABC-123" {
			t.Fatalf("expected fields.data=ABC-123, got %v", data)
		}
		if body.Options.Width != 320 || body.Options.Height != 110 {
			t.Fatalf("unexpected options: %+v", body.Options)
		}

		_ = json.NewEncoder(w).Encode(domain.GenerateCode128Response{
			Success:    true,
			BarcodeURL: "https://cdn.example.com/code128.png",
			Format:     "code128",
			Metadata:   domain.BarcodeMetadata{EncodedData: "ABC-123"},
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second)
	resp, err := client.GenerateCode128(context.Background(), domain.GenerateCode128Request{
		Data:           "ABC-123",
		Options:        domain.BarcodeOptions{Width: 320, Height: 110},
		IdempotencyKey: "code128-key-1",
	})
	if err != nil {
		t.Fatalf("GenerateCode128 returned error: %v", err)
	}
	if !resp.Success || resp.Format != "code128" || resp.Metadata.EncodedData != "ABC-123" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHTTPClient_CalculateAndRandomDecodeValueResponses(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/calculate":
			var body calculateReqBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode calculate request: %v", err)
			}
			if body.Revision != "US_CA_08292017" || body.Field != "DAQ" {
				t.Fatalf("unexpected calculate body: %+v", body)
			}
			_ = json.NewEncoder(w).Encode(valueRespBody{Value: "CALCULATED"})
		case "/api/v1/random":
			var body randomReqBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode random request: %v", err)
			}
			if body.Field != "DAE" || body.Params["type"] != "date" {
				t.Fatalf("unexpected random body: %+v", body)
			}
			_ = json.NewEncoder(w).Encode(valueRespBody{Value: "2026-01-15"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second)
	calculated, err := client.Calculate(context.Background(), "US_CA_08292017", "DAQ", map[string]any{"firstName": "JOHN"})
	if err != nil {
		t.Fatalf("Calculate returned error: %v", err)
	}
	if calculated != "CALCULATED" {
		t.Fatalf("expected CALCULATED, got %#v", calculated)
	}

	random, err := client.Random(context.Background(), "US_CA_08292017", "DAE", map[string]any{"type": "date"})
	if err != nil {
		t.Fatalf("Random returned error: %v", err)
	}
	if random != "2026-01-15" {
		t.Fatalf("expected 2026-01-15, got %#v", random)
	}
}

func TestHTTPClient_GeneratePostsBarcodeTypeAndFields(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/generate" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body generateReqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode generate request: %v", err)
		}
		if body.BarcodeType != "pdf417" || body.Fields["firstName"] != "JOHN" {
			t.Fatalf("unexpected generate body: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(generateRespBody{URL: "https://cdn.example.com/generic.png", Format: "pdf417"})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second)
	item, err := client.Generate(context.Background(), "pdf417", map[string]any{"firstName": "JOHN"})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if item.URL == "" || item.Format != "pdf417" {
		t.Fatalf("unexpected barcode item: %+v", item)
	}
}

func TestHTTPClient_ReturnsStatusErrorAndUsesDynamicTimeout(t *testing.T) {
	t.Parallel()

	t.Run("status error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad gateway", http.StatusBadGateway)
		}))
		defer server.Close()

		client := NewHTTPClient(server.URL, time.Second)
		_, err := client.Generate(context.Background(), "pdf417", map[string]any{"x": 1})
		if err == nil {
			t.Fatal("expected error for non-2xx status")
		}
		if !strings.Contains(err.Error(), "/api/v1/generate") || !strings.Contains(err.Error(), "status 502") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("dynamic timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(80 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(valueRespBody{Value: "late"})
		}))
		defer server.Close()

		client := NewHTTPClient(server.URL, time.Second).
			WithTimeouts(timeouts.NewMemoryStore(20*time.Millisecond, time.Second, time.Second, time.Second, time.Second))
		_, err := client.Calculate(context.Background(), "US_CA_08292017", "DAQ", map[string]any{"x": 1})
		if err == nil {
			t.Fatal("expected timeout error")
		}
		if !strings.Contains(err.Error(), "context deadline exceeded") {
			t.Fatalf("expected deadline exceeded, got %v", err)
		}
	})
}

func TestHTTPClient_GenerateCode128ReturnsStatusError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unprocessable", http.StatusUnprocessableEntity)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second)
	_, err := client.GenerateCode128(context.Background(), domain.GenerateCode128Request{Data: "ABC-123"})
	if err == nil {
		t.Fatal("expected error for non-2xx status")
	}
	if !strings.Contains(err.Error(), "/api/v1/generate/code128") || !strings.Contains(err.Error(), "status 422") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMockClient_GenerateCalculateAndRandom(t *testing.T) {
	t.Parallel()

	client := NewMockClient()

	item1, err := client.Generate(context.Background(), "pdf417", nil)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	item2, err := client.Generate(context.Background(), "pdf417", nil)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if item1.URL == item2.URL {
		t.Fatalf("expected unique URLs, got %q and %q", item1.URL, item2.URL)
	}
	if item1.Format != "pdf417" {
		t.Fatalf("unexpected format: %+v", item1)
	}

	calculated, err := client.Calculate(context.Background(), "US_CA_08292017", "DAQ", map[string]any{"firstName": "JOHN", "lastName": "DOE"})
	if err != nil {
		t.Fatalf("Calculate returned error: %v", err)
	}
	if calculated != "DJD001" {
		t.Fatalf("expected DAQ calculation DJD001, got %#v", calculated)
	}

	defaultCalculated, err := client.Calculate(context.Background(), "US_CA_08292017", "ZZZ", map[string]any{})
	if err != nil {
		t.Fatalf("Calculate returned error: %v", err)
	}
	if defaultCalculated != "CALC_US_CA_08292017_ZZZ" {
		t.Fatalf("unexpected default calculated value: %#v", defaultCalculated)
	}

	randomDate, err := client.Random(context.Background(), "US_CA_08292017", "DAE", map[string]any{"type": "date"})
	if err != nil {
		t.Fatalf("Random returned error: %v", err)
	}
	if randomDate != "2026-01-15" {
		t.Fatalf("unexpected random date: %#v", randomDate)
	}

	randomDefault, err := client.Random(context.Background(), "US_CA_08292017", "DBD", map[string]any{})
	if err != nil {
		t.Fatalf("Random returned error: %v", err)
	}
	if randomDefault != "RANDOM_DBD" {
		t.Fatalf("unexpected default random value: %#v", randomDefault)
	}
}

func TestMockClient_GeneratePDF417AndCode128UseDefaultOptions(t *testing.T) {
	t.Parallel()

	client := NewMockClient()

	pdfResp, err := client.GeneratePDF417(context.Background(), domain.GeneratePDF417Request{})
	if err != nil {
		t.Fatalf("GeneratePDF417 returned error: %v", err)
	}
	if !pdfResp.Success || pdfResp.Format != "pdf417" || pdfResp.Metadata.Width != 400 || pdfResp.Metadata.Height != 150 {
		t.Fatalf("unexpected pdf417 response: %+v", pdfResp)
	}

	codeResp, err := client.GenerateCode128(context.Background(), domain.GenerateCode128Request{Data: "ABC-123"})
	if err != nil {
		t.Fatalf("GenerateCode128 returned error: %v", err)
	}
	if !codeResp.Success || codeResp.Format != "code128" || codeResp.Metadata.Width != 300 || codeResp.Metadata.Height != 100 || codeResp.Metadata.EncodedData != "ABC-123" {
		t.Fatalf("unexpected code128 response: %+v", codeResp)
	}
}
