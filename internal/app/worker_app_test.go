package app

import (
	"context"
	"testing"

	kafkaadapter "github.com/ikermy/BFF/internal/adapters/kafka"
	"github.com/ikermy/BFF/internal/config"
)

type spyBulkJobConsumer struct {
	started bool
	ctx     context.Context
	err     error
}

func (s *spyBulkJobConsumer) Start(ctx context.Context) error {
	s.started = true
	s.ctx = ctx
	return s.err
}

func (s *spyBulkJobConsumer) PendingCount() int { return 0 }

func TestBuildWorkerApp_UsesMockConsumerWithoutKafkaEnv(t *testing.T) {
	t.Setenv(config.EnvKafkaBrokers, "")
	t.Setenv(config.EnvBillingURL, "")
	t.Setenv(config.EnvBarcodeGenURL, "")
	t.Setenv(config.EnvAIURL, "")

	worker := BuildWorkerApp(config.Load())
	if worker == nil {
		t.Fatal("expected worker app")
	}
	if _, ok := worker.consumer.(*kafkaadapter.MockConsumer); !ok {
		t.Fatalf("expected mock consumer, got %T", worker.consumer)
	}
}

func TestBuildWorkerApp_UsesRealConsumerWhenKafkaEnvSet(t *testing.T) {
	t.Setenv(config.EnvKafkaBrokers, "127.0.0.1:9092")
	t.Setenv(config.EnvBillingURL, "")
	t.Setenv(config.EnvBarcodeGenURL, "")
	t.Setenv(config.EnvAIURL, "")

	worker := BuildWorkerApp(config.Load())
	if worker == nil {
		t.Fatal("expected worker app")
	}
	if _, ok := worker.consumer.(*kafkaadapter.Consumer); !ok {
		t.Fatalf("expected real consumer, got %T", worker.consumer)
	}
}

func TestWorkerApp_RunDelegatesToConsumerStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	consumer := &spyBulkJobConsumer{}
	worker := &WorkerApp{consumer: consumer}

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !consumer.started {
		t.Fatal("expected consumer Start to be called")
	}
	if consumer.ctx != ctx {
		t.Fatal("expected worker to pass original context to consumer")
	}
}

func TestWorkerApp_RunReturnsNilOnAlreadyCancelledContext(t *testing.T) {
	t.Setenv(config.EnvKafkaBrokers, "")
	t.Setenv(config.EnvBillingURL, "")
	t.Setenv(config.EnvBarcodeGenURL, "")
	t.Setenv(config.EnvAIURL, "")

	worker := BuildWorkerApp(config.Load())
	if worker == nil {
		t.Fatal("expected worker app")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("expected nil error for cancelled context, got %v", err)
	}
}
