package kafka

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/ikermy/BFF/internal/adapters/barcodegen"
	"github.com/ikermy/BFF/internal/adapters/billing"
	"github.com/ikermy/BFF/internal/adapters/revisions"
	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/usecase"
)

type spyEventPublisher struct {
	mu          sync.Mutex
	bulkResults []domain.BulkResultEvent
}

func (s *spyEventPublisher) PublishSagaCompleted(context.Context, string) error { return nil }

func (s *spyEventPublisher) PublishBulkResult(_ context.Context, event domain.BulkResultEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bulkResults = append(s.bulkResults, event)
	return nil
}

func (s *spyEventPublisher) PublishPartialCompleted(context.Context, domain.PartialCompletedEvent) error {
	return nil
}

func (s *spyEventPublisher) PublishBarcodeEdited(context.Context, domain.BarcodeEditedEvent) error {
	return nil
}

func (s *spyEventPublisher) PublishBarcodeGenerated(context.Context, domain.BarcodeGeneratedEvent) error {
	return nil
}

func (s *spyEventPublisher) snapshotBulkResults() []domain.BulkResultEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.BulkResultEvent, len(s.bulkResults))
	copy(out, s.bulkResults)
	return out
}

func newTestBulkHandler() (*BulkJobHandler, *spyEventPublisher) {
	publisher := &spyEventPublisher{}
	billingClient := billing.NewMockClient(1.0)
	barcodeClient := barcodegen.NewMockClient()
	revisionStore := revisions.NewMemoryStore()
	quoteCase := usecase.NewQuoteUseCase(billingClient)
	chainExecutor := usecase.NewChainExecutor(barcodeClient, revisionStore)
	generateCase := usecase.NewGenerateUseCase(billingClient, barcodeClient, publisher, quoteCase).
		WithChainExecutor(chainExecutor).
		WithRevisionStore(revisionStore)
	return NewBulkJobHandler(generateCase, publisher), publisher
}

func validBulkFields() map[string]any {
	return map[string]any{
		"firstName":   "JOHN",
		"lastName":    "DOE",
		"dateOfBirth": "1990-05-15",
		"street":      "123 Main St",
		"city":        "Los Angeles",
		"state":       "CA",
		"zipCode":     "90001",
	}
}

func TestBulkJobHandler_HandleSuccess(t *testing.T) {
	handler, publisher := newTestBulkHandler()

	msg := domain.BulkJobMessage{
		BatchID: "batch-42",
		UserID:  "user-1",
		Items: []domain.BulkJobItem{{
			JobID:     "job-1",
			RowNumber: 7,
			Revision:  "US_CA_08292017",
			Fields:    validBulkFields(),
		}},
	}

	if err := handler.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	results := publisher.snapshotBulkResults()
	if len(results) != 1 {
		t.Fatalf("expected 1 bulk result, got %d", len(results))
	}

	event := results[0]
	if event.JobID != "job-1" {
		t.Fatalf("expected jobID job-1, got %q", event.JobID)
	}
	if event.Status != "COMPLETED" {
		t.Fatalf("expected COMPLETED status, got %q", event.Status)
	}
	if event.BuildID != "bulk-job-1" {
		t.Fatalf("expected buildID bulk-job-1, got %q", event.BuildID)
	}
	if event.BatchID != "batch-42" {
		t.Fatalf("expected batchID batch-42, got %q", event.BatchID)
	}
	if len(event.BarcodeURLs) != 1 {
		t.Fatalf("expected 1 barcode URL, got %d", len(event.BarcodeURLs))
	}
	if url := event.BarcodeURLs["row_7_0"]; !strings.Contains(url, "pdf417") {
		t.Fatalf("expected row_7_0 url containing pdf417, got %q", url)
	}
}

func TestBulkJobHandler_HandleContinuesAfterItemFailure(t *testing.T) {
	handler, publisher := newTestBulkHandler()

	msg := domain.BulkJobMessage{
		BatchID: "batch-77",
		UserID:  "user-2",
		Items: []domain.BulkJobItem{
			{
				JobID:     "job-fail",
				RowNumber: 1,
				Revision:  "US_CA_08292017",
				Fields: map[string]any{
					"lastName": "DOE",
				},
			},
			{
				JobID:     "job-ok",
				RowNumber: 2,
				Revision:  "US_CA_08292017",
				Fields:    validBulkFields(),
			},
		},
	}

	err := handler.Handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error for failed item")
	}
	if !strings.Contains(err.Error(), "jobId=job-fail") {
		t.Fatalf("expected error to mention first failed job, got %v", err)
	}

	results := publisher.snapshotBulkResults()
	if len(results) != 2 {
		t.Fatalf("expected 2 bulk results, got %d", len(results))
	}

	byJobID := make(map[string]domain.BulkResultEvent, len(results))
	for _, event := range results {
		byJobID[event.JobID] = event
	}

	if byJobID["job-fail"].Status != "FAILED" {
		t.Fatalf("expected job-fail status FAILED, got %q", byJobID["job-fail"].Status)
	}
	if len(byJobID["job-fail"].BarcodeURLs) != 0 {
		t.Fatalf("expected failed item to have no barcode URLs, got %#v", byJobID["job-fail"].BarcodeURLs)
	}
	if byJobID["job-ok"].Status != "COMPLETED" {
		t.Fatalf("expected job-ok status COMPLETED, got %q", byJobID["job-ok"].Status)
	}
	if byJobID["job-ok"].BuildID != "bulk-job-ok" {
		t.Fatalf("expected job-ok buildID bulk-job-ok, got %q", byJobID["job-ok"].BuildID)
	}
	if _, ok := byJobID["job-ok"].BarcodeURLs["row_2_0"]; !ok {
		t.Fatalf("expected completed item to contain row_2_0 URL, got %#v", byJobID["job-ok"].BarcodeURLs)
	}
}

func TestBulkJobHandler_HandleEmptyItems(t *testing.T) {
	handler, publisher := newTestBulkHandler()

	msg := domain.BulkJobMessage{BatchID: "empty-batch", UserID: "user-3"}
	if err := handler.Handle(context.Background(), msg); err != nil {
		t.Fatalf("expected nil error for empty batch, got %v", err)
	}
	if results := publisher.snapshotBulkResults(); len(results) != 0 {
		t.Fatalf("expected no bulk results for empty batch, got %d", len(results))
	}
}
