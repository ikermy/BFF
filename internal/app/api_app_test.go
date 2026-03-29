package app

import (
	"context"
	"testing"

	"github.com/ikermy/BFF/internal/config"
)

func TestBuildAPIApp_UsesMockDependenciesWhenEnvIsUnset(t *testing.T) {
	t.Setenv(config.EnvRedisURL, "")
	t.Setenv(config.EnvAuthURL, "")
	t.Setenv(config.EnvBillingURL, "")
	t.Setenv(config.EnvBarcodeGenURL, "")
	t.Setenv(config.EnvAIURL, "")
	t.Setenv(config.EnvHistoryURL, "")
	t.Setenv(config.EnvKafkaBrokers, "")

	app := BuildAPIAppWithContext(context.Background(), config.Load())
	if app == nil {
		t.Fatal("expected API app")
	}
	if app.server == nil {
		t.Fatal("expected server to be initialized")
	}
}

func TestBuildAPIApp_UsesConfiguredRealBranchesWhenEnvIsSet(t *testing.T) {
	t.Setenv(config.EnvRedisURL, "redis://localhost:6379/0")
	t.Setenv(config.EnvAuthURL, "http://auth-service:3000")
	t.Setenv(config.EnvBillingURL, "http://billing:3000")
	t.Setenv(config.EnvBarcodeGenURL, "http://barcodegen:8080")
	t.Setenv(config.EnvAIURL, "http://ai-service:8080")
	t.Setenv(config.EnvHistoryURL, "http://history:3000")
	t.Setenv(config.EnvKafkaBrokers, "127.0.0.1:9092")

	app := BuildAPIAppWithContext(context.Background(), config.Load())
	if app == nil {
		t.Fatal("expected API app")
	}
	if app.server == nil {
		t.Fatal("expected server to be initialized")
	}
}
