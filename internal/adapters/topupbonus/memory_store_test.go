package topupbonus

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ikermy/BFF/internal/domain"
)

func TestMemoryStore_LoadFromFileBootstrapsDefaultConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "topup-bonus.yaml")
	store := NewMemoryStore()

	if err := store.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile returned error: %v", err)
	}

	cfg, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("expected default topup bonus to be disabled")
	}
	if len(cfg.Tiers) != 0 {
		t.Fatalf("expected no default tiers, got %+v", cfg.Tiers)
	}
}

func TestMemoryStore_SetPersistsToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "topup-bonus.yaml")
	store := NewMemoryStore()
	if err := store.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile returned error: %v", err)
	}

	m := 100.0
	updated := domain.TopupBonusConfig{
		Enabled: true,
		Tiers:   []domain.TopupBonusTier{{MinAmount: 0, MaxAmount: &m, BonusPercent: 5}},
	}
	if err := store.Set(context.Background(), updated); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	reloaded := NewMemoryStore()
	if err := reloaded.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile reloaded returned error: %v", err)
	}
	cfg, err := reloaded.Get(context.Background())
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !cfg.Enabled || len(cfg.Tiers) != 1 || cfg.Tiers[0].BonusPercent != 5 {
		t.Fatalf("unexpected persisted topup bonus config: %+v", cfg)
	}
	if cfg.Tiers[0].MaxAmount == nil || *cfg.Tiers[0].MaxAmount != 100 {
		t.Fatalf("expected persisted maxAmount=100, got %+v", cfg.Tiers[0].MaxAmount)
	}
}
