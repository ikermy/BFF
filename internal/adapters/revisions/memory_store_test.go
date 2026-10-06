package revisions

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikermy/BFF/internal/domain"
)

func TestMemoryStore_EmptyByDefault(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore()

	if err := store.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "US_CA_08292017.yaml")); !os.IsNotExist(err) {
		t.Fatalf("did not expect LoadFromDir to create yaml files automatically, got err=%v", err)
	}
	// ПЛАН §3.1: hardcoded определения удалены — пустой каталог даёт пустой store.
	if _, err := store.GetConfig(context.Background(), "US_CA_08292017"); err == nil {
		t.Fatal("expected no in-memory default config, got one")
	}
}

func TestMemoryStore_UpdateConfigPersistsToYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "US_CA_08292017.yaml")
	content := "name: US_CA_08292017\n" +
		"displayName: \"California Driver License 2017\"\n" +
		"enabled: true\n" +
		"requiredInputFields:\n" +
		"  - firstName\n" +
		"  - lastName\n" +
		"  - dateOfBirth\n" +
		"generationSteps:\n" +
		"  - id: daq\n" +
		"    endpoint: random\n" +
		"    input: [DAJ]\n" +
		"    output: [DAQ]\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	store := NewMemoryStore()
	if err := store.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir returned error: %v", err)
	}

	update := domain.UpdateRevisionRequest{
		Enabled: false,
		// CalculationChain устарел и игнорируется admin-ом (static definition).
		CalculationChain: []domain.ChainEntry{
			{Field: "DAE", Source: "random", Params: map[string]any{"type": "date"}},
		},
	}
	if err := store.UpdateConfig(context.Background(), "US_CA_08292017", update); err != nil {
		t.Fatalf("UpdateConfig returned error: %v", err)
	}

	reloaded := NewMemoryStore()
	if err := reloaded.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir reloaded returned error: %v", err)
	}
	cfg, err := reloaded.GetConfig(context.Background(), "US_CA_08292017")
	if err != nil {
		t.Fatalf("GetConfig returned error: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("expected enabled=false after reload")
	}
	// ПЛАН §3.1: admin не перезаписывает статический definition — steps сохраняются.
	if len(cfg.GenerationSteps) != 1 || cfg.GenerationSteps[0].ID != "daq" {
		t.Fatalf("generationSteps must be preserved, got: %+v", cfg.GenerationSteps)
	}
	if cfg.DisplayName == "" || len(cfg.RequiredInputFields) != 3 {
		t.Fatalf("expected displayName and requiredInputFields to be preserved, got %+v", cfg)
	}
}
