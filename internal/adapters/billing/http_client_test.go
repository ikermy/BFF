package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ikermy/BFF/internal/adapters/timeouts"
	"github.com/ikermy/BFF/internal/domain"
)

func TestHTTPClient_QuoteComputesShortfallAndSendsExpectedPayload(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/internal/billing/quote" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}

		var body quoteReqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.UserID != "user-1" || body.Units != 5 {
			t.Fatalf("unexpected request body: %+v", body)
		}
		if body.Context.Product != "barcode" || body.Context.Revision != "US_CA_08292017" {
			t.Fatalf("unexpected quote context: %+v", body.Context)
		}

		_ = json.NewEncoder(w).Encode(quoteRespBody{
			AllowedTotal: 3,
			UnitPrice:    0.5,
			BySource: domain.QuoteBreakdown{
				Subscription: domain.SourceBreakdown{Units: 2},
				Credits:      domain.SourceBreakdown{Units: 1},
				Wallet:       domain.SourceBreakdown{Units: 0, Amount: 0},
			},
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	result, err := client.Quote(context.Background(), "user-1", 5, "US_CA_08292017")
	if err != nil {
		t.Fatalf("Quote returned error: %v", err)
	}

	if !result.CanProcess {
		t.Fatal("expected CanProcess=true")
	}
	if !result.Partial {
		t.Fatal("expected Partial=true")
	}
	if result.Requested != 5 || result.AllowedTotal != 3 {
		t.Fatalf("unexpected quote result counters: %+v", result)
	}
	if result.Shortfall == nil {
		t.Fatal("expected shortfall to be computed")
	}
	if result.Shortfall.Units != 2 || result.Shortfall.AmountRequired != 1.0 {
		t.Fatalf("unexpected shortfall: %+v", result.Shortfall)
	}
}

func TestHTTPClient_BlockReturnsStatusError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "billing unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	err := client.Block(context.Background(), domain.BlockRequest{
		UserID: "user-1",
		Units:  1,
		SagaID: "saga-1",
	})
	if err == nil {
		t.Fatal("expected error for non-2xx status")
	}
	if !strings.Contains(err.Error(), "/internal/billing/block") || !strings.Contains(err.Error(), "status 503") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_BlockBatchDecodesTransactionIDs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/billing/block-batch" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body blockBatchReqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.UserID != "user-1" || body.Count != 2 || body.BatchID != "batch-1" {
			t.Fatalf("unexpected block-batch body: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(blockBatchRespBody{TransactionIDs: []string{"tx-1", "tx-2"}})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	ids, err := client.BlockBatch(context.Background(), "user-1", 2, "batch-1")
	if err != nil {
		t.Fatalf("BlockBatch returned error: %v", err)
	}
	if len(ids) != 2 || ids[0] != "tx-1" || ids[1] != "tx-2" {
		t.Fatalf("unexpected transaction ids: %#v", ids)
	}
}

func TestHTTPClient_UsesDynamicTimeoutStore(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL).WithTimeouts(timeouts.NewMemoryStore(time.Second, 20*time.Millisecond, time.Second, time.Second, time.Second))
	err := client.Capture(context.Background(), "saga-1", 1)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestHTTPClient_ReleaseAndBlockSendExpectedPayloads(t *testing.T) {
	t.Parallel()

	var seenBlock bool
	var seenRelease bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/billing/block":
			seenBlock = true
			var body blockReqBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode block request: %v", err)
			}
			if body.UserID != "user-9" || body.Units != 2 || body.SagaID != "saga-9" || body.BuildID != "build-9" || body.BatchID != "batch-9" {
				t.Fatalf("unexpected block body: %+v", body)
			}
			if body.BySource.Subscription.Units != 1 || body.BySource.Credits.Units != 1 {
				t.Fatalf("unexpected bySource: %+v", body.BySource)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/internal/billing/release":
			seenRelease = true
			var body releaseReqBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode release request: %v", err)
			}
			if body.SagaID != "saga-9" || body.Units != 1 {
				t.Fatalf("unexpected release body: %+v", body)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	if err := client.Block(context.Background(), domain.BlockRequest{
		UserID: "user-9",
		Units:  2,
		BySource: domain.QuoteBreakdown{
			Subscription: domain.SourceBreakdown{Units: 1},
			Credits:      domain.SourceBreakdown{Units: 1},
		},
		SagaID:  "saga-9",
		BuildID: "build-9",
		BatchID: "batch-9",
	}); err != nil {
		t.Fatalf("Block returned error: %v", err)
	}
	if err := client.Release(context.Background(), "saga-9", 1); err != nil {
		t.Fatalf("Release returned error: %v", err)
	}
	if !seenBlock || !seenRelease {
		t.Fatalf("expected both endpoints to be called, block=%v release=%v", seenBlock, seenRelease)
	}
}

func TestMockClient_QuoteWaterfallAndShortfall(t *testing.T) {
	t.Parallel()

	// UnitPrice используется как ratio: 0.5 → 50% доступного баланса.
	// units=120, ratio=0.5 → allowed=60 (partial), shortfall=60.
	// Waterfall на 60 единицах: sub=30, cred=20, wallet=10.
	client := NewMockClient(0.5)
	result, err := client.Quote(context.Background(), "user-1", 120, "US_CA_08292017")
	if err != nil {
		t.Fatalf("Quote returned error: %v", err)
	}

	if !result.CanProcess || !result.Partial {
		t.Fatalf("expected partial processable quote, got %+v", result)
	}
	if result.AllowedTotal != 60 {
		t.Fatalf("expected allowedTotal=60, got %d", result.AllowedTotal)
	}
	// Waterfall на 60 единицах: sub=30, cred=20, wallet=10
	if result.BySource.Subscription.Units != 30 || result.BySource.Credits.Units != 20 || result.BySource.Wallet.Units != 10 {
		t.Fatalf("unexpected waterfall breakdown: %+v", result.BySource)
	}
	// wallet.Amount = 10 * 0.50 (фиксированная цена)
	if result.BySource.Wallet.Amount != 5 {
		t.Fatalf("expected wallet amount 5, got %v", result.BySource.Wallet.Amount)
	}
	// shortfall = 120 - 60 = 60 единиц, amount = 60 * 0.50 = 30
	if result.Shortfall == nil || result.Shortfall.Units != 60 || result.Shortfall.AmountRequired != 30 {
		t.Fatalf("unexpected shortfall: %+v", result.Shortfall)
	}
}

func TestMockClient_BlockCaptureReleaseAndBlockBatchValidation(t *testing.T) {
	t.Parallel()

	client := NewMockClient(0.5)

	if err := client.Block(context.Background(), domain.BlockRequest{Units: 1}); err == nil || !strings.Contains(err.Error(), "sagaID is required") {
		t.Fatalf("expected sagaID validation error, got %v", err)
	}
	if err := client.Block(context.Background(), domain.BlockRequest{SagaID: "saga-1", Units: -1}); err == nil || !strings.Contains(err.Error(), "units must be >= 0") {
		t.Fatalf("expected units validation error, got %v", err)
	}
	if err := client.Block(context.Background(), domain.BlockRequest{SagaID: "saga-1", Units: 0}); err != nil {
		t.Fatalf("expected free-edit block to succeed, got %v", err)
	}

	if err := client.Capture(context.Background(), "", 0); err == nil || !strings.Contains(err.Error(), "invalid capture request") {
		t.Fatalf("expected capture validation error, got %v", err)
	}
	if err := client.Capture(context.Background(), "saga-1", 2); err != nil {
		t.Fatalf("Capture returned error: %v", err)
	}

	if err := client.Release(context.Background(), "", 0); err == nil || !strings.Contains(err.Error(), "invalid release request") {
		t.Fatalf("expected release validation error, got %v", err)
	}
	if err := client.Release(context.Background(), "saga-1", 1); err != nil {
		t.Fatalf("Release returned error: %v", err)
	}

	if _, err := client.BlockBatch(context.Background(), "user-1", 0, ""); err == nil || !strings.Contains(err.Error(), "invalid block-batch request") {
		t.Fatalf("expected block-batch validation error, got %v", err)
	}
	ids, err := client.BlockBatch(context.Background(), "user-1", 3, "batch-1")
	if err != nil {
		t.Fatalf("BlockBatch returned error: %v", err)
	}
	if len(ids) != 3 || ids[0] != "batch-1-tx-1" || ids[2] != "batch-1-tx-3" {
		t.Fatalf("unexpected transaction ids: %#v", ids)
	}
}
