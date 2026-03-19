package config

import (
	"testing"
	"time"
)

func TestLoad_IdempotencyTTLFromSeconds(t *testing.T) {
	t.Setenv(EnvIdempotencyTTL, "86400")

	cfg := Load()
	if cfg.Idempotency.TTL != 24*time.Hour {
		t.Fatalf("expected IDEMPOTENCY_TTL=86400 to equal 24h, got %v", cfg.Idempotency.TTL)
	}
}

func TestLoad_TimeoutsStillUseMilliseconds(t *testing.T) {
	t.Setenv(EnvBarcodeGenTimeout, "30000")
	t.Setenv(EnvBillingTimeout, "5000")
	t.Setenv(EnvAITimeout, "60000")

	cfg := Load()
	if cfg.Timeouts.BarcodeGen != 30*time.Second {
		t.Fatalf("expected BARCODEGEN_TIMEOUT=30000 to equal 30s, got %v", cfg.Timeouts.BarcodeGen)
	}
	if cfg.Timeouts.Billing != 5*time.Second {
		t.Fatalf("expected BILLING_TIMEOUT=5000 to equal 5s, got %v", cfg.Timeouts.Billing)
	}
	if cfg.Timeouts.AI != 60*time.Second {
		t.Fatalf("expected AI_TIMEOUT=60000 to equal 60s, got %v", cfg.Timeouts.AI)
	}
}
