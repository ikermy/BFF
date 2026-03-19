package revisions

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikermy/BFF/internal/domain"
)

func TestMemoryStore_LoadFromDirBootstrapsDefaultsIntoEmptyDir(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore()

	if err := store.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "US_CA_08292017.yaml")); !os.IsNotExist(err) {
		t.Fatalf("did not expect LoadFromDir to create yaml files automatically, got err=%v", err)
	}
	if _, err := store.GetConfig(context.Background(), "US_CA_08292017"); err != nil {
		t.Fatalf("expected default in-memory revision config to remain available, got %v", err)
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
		"calculationChain:\n" +
		"  - field: DAQ\n" +
		"    source: calculate\n" +
		"    dependsOn: [firstName, lastName, dateOfBirth]\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	store := NewMemoryStore()
	if err := store.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir returned error: %v", err)
	}

	update := domain.UpdateRevisionRequest{
		Enabled: false,
		CalculationChain: []domain.ChainEntry{
			{Field: "DAQ", Source: "calculate", DependsOn: []string{"firstName", "lastName", "dateOfBirth"}},
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
	if len(cfg.CalculationChain) != 2 || cfg.CalculationChain[1].Field != "DAE" || cfg.CalculationChain[1].Source != "random" {
		t.Fatalf("unexpected calculationChain after reload: %+v", cfg.CalculationChain)
	}
	if cfg.DisplayName == "" || len(cfg.RequiredInputFields) != 3 {
		t.Fatalf("expected displayName and requiredInputFields to be preserved, got %+v", cfg)
	}
}
