package timeouts

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikermy/BFF/internal/domain"
)

func TestMemoryStore_LoadFromFileBootstrapsDefaultConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timeouts.yaml")
	store := NewMemoryStore(30*time.Second, 5*time.Second, 60*time.Second, 5*time.Second, 5*time.Second)

	if err := store.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile returned error: %v", err)
	}

	cfg, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if cfg.BarcodeGen != 30000 || cfg.Billing != 5000 || cfg.AI != 60000 {
		t.Fatalf("unexpected bootstrapped config: %+v", cfg)
	}
	if cfg.History != 5000 || cfg.Auth != 5000 {
		t.Fatalf("unexpected history/auth defaults: history=%d auth=%d", cfg.History, cfg.Auth)
	}
}

func TestMemoryStore_SetPersistsToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timeouts.yaml")
	store := NewMemoryStore(30*time.Second, 5*time.Second, 60*time.Second, 5*time.Second, 5*time.Second)
	if err := store.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile returned error: %v", err)
	}

	updated := domain.ServiceTimeouts{BarcodeGen: 45000, Billing: 7000, AI: 90000, History: 3000, Auth: 4000}
	if err := store.Set(context.Background(), updated); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	reloaded := NewMemoryStore(time.Second, time.Second, time.Second, time.Second, time.Second)
	if err := reloaded.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile reloaded returned error: %v", err)
	}
	cfg, err := reloaded.Get(context.Background())
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if cfg.BarcodeGen != updated.BarcodeGen || cfg.Billing != updated.Billing || cfg.AI != updated.AI {
		t.Fatalf("unexpected persisted config: %+v", cfg)
	}
	if cfg.History != updated.History || cfg.Auth != updated.Auth {
		t.Fatalf("unexpected persisted history/auth: history=%d auth=%d", cfg.History, cfg.Auth)
	}
}
