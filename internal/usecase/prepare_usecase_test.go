package usecase

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikermy/BFF/internal/adapters/barcodegen"
	"github.com/ikermy/BFF/internal/adapters/revisions"
	"github.com/ikermy/BFF/internal/domain"
)

func TestPrepareUseCase_ReturnsDraft(t *testing.T) {
	store := groupedStore(t)
	clk := fixedClock{t: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)}
	chain := NewChainExecutor(barcodegen.NewMockClient(), store).WithDeriver(&scriptedDeriver{}).WithClock(clk)
	uc := NewPrepareUseCase(chain, store, store)

	resp, err := uc.Execute(context.Background(), domain.PrepareRequest{
		Revision: "US_GROUP_01012020",
		Fields:   map[string]any{"DAJ": "AR", "DBB": "07191994"},
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !resp.Success || resp.Revision != "US_GROUP_01012020" {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.DraftFields["DBD"] != "03072022" || resp.DraftFields["DCK"] != "INV-1" {
		t.Fatalf("draft = %v", resp.DraftFields)
	}
}

func TestPrepareUseCase_UnsupportedMode(t *testing.T) {
	dir := t.TempDir()
	doc := `name: US_NOPREPARE
displayName: no prepare
supportedModes: [prepared]
generationSteps:
  - id: inv
    endpoint: calculate
    input: [DAJ]
    output: [DCK]
`
	if err := os.WriteFile(filepath.Join(dir, "US_NOPREPARE.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	store := revisions.NewMemoryStore()
	if err := store.LoadFromDir(dir); err != nil {
		t.Fatal(err)
	}
	chain := NewChainExecutor(barcodegen.NewMockClient(), store)
	uc := NewPrepareUseCase(chain, store, store)

	_, err := uc.Execute(context.Background(), domain.PrepareRequest{Revision: "US_NOPREPARE"})
	if err == nil {
		t.Fatal("expected capability error for unsupported prepare mode")
	}
}
