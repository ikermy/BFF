package app

import (
	"context"
	"log"
	"os"
	"time"

	aiadapter "github.com/ikermy/BFF/internal/adapters/ai"
	"github.com/ikermy/BFF/internal/adapters/barcodegen"
	"github.com/ikermy/BFF/internal/adapters/billing"
	"github.com/ikermy/BFF/internal/adapters/events"
	kafkaadapter "github.com/ikermy/BFF/internal/adapters/kafka"
	"github.com/ikermy/BFF/internal/adapters/revisions"
	"github.com/ikermy/BFF/internal/adapters/timeouts"
	"github.com/ikermy/BFF/internal/config"
	"github.com/ikermy/BFF/internal/ports"
	kafkatransport "github.com/ikermy/BFF/internal/transport/kafka"
	"github.com/ikermy/BFF/internal/usecase"
)

type WorkerApp struct {
	consumer ports.BulkJobConsumer
	producer *kafkaadapter.Producer // nil если используется mock
}

func (w *WorkerApp) Run(ctx context.Context) error {
	log.Printf("worker started, listening on bulk.tasks topic")
	return w.consumer.Start(ctx)
}

// Close освобождает ресурсы воркера: закрывает Kafka Writer.
// Вызывается после Run() при graceful shutdown.
func (w *WorkerApp) Close() {
	if w.producer != nil {
		if err := w.producer.Close(); err != nil {
			log.Printf("warn: kafka producer close: %v", err)
		}
	}
}

func BuildWorkerApp(cfg config.Config) *WorkerApp {
	// ── Stores — объявляются первыми ─────────────────────────────────────────
	timeoutStore := timeouts.NewMemoryStore(cfg.Timeouts.BarcodeGen, cfg.Timeouts.Billing, cfg.Timeouts.AI, 5*time.Second, 5*time.Second)
	revisionStore := revisions.NewMemoryStore()
	if err := timeoutStore.LoadFromFile(config.TimeoutsConfigPath); err != nil {
		log.Printf("warn: cannot load timeouts config from %s: %v (using defaults)", config.TimeoutsConfigPath, err)
	}
	if err := revisionStore.LoadFromDir(config.RevisionsDirPath); err != nil {
		log.Printf("warn: cannot load revisions from yaml: %v (using defaults)", err)
	}

	// ── HTTP-клиенты ─────────────────────────────────────────────────────────

	// Billing: HTTPClient если BILLING_URL задан явно, иначе Mock (п.7 ТЗ).
	var billingClient ports.BillingClient
	if url := os.Getenv(config.EnvBillingURL); url != "" {
		log.Printf("worker billing: using HTTP client → %s", url)
		billingClient = billing.NewHTTPClient(cfg.Services.BillingURL).
			WithTimeouts(timeoutStore)
	} else {
		log.Printf("worker billing: BILLING_URL not set, using mock client")
		billingClient = billing.NewMockClient(cfg.UnitPrice)
	}

	// BarcodeGen: HTTPClient если BARCODEGEN_URL задан явно, иначе Mock (п.8 ТЗ).
	var barcodeClient ports.BarcodeGenClient
	if url := os.Getenv(config.EnvBarcodeGenURL); url != "" {
		log.Printf("worker barcodegen: using HTTP client → %s", url)
		barcodeClient = barcodegen.NewHTTPClient(cfg.Services.BarcodeGenURL, cfg.Timeouts.BarcodeGen).
			WithTimeouts(timeoutStore)
	} else {
		log.Printf("worker barcodegen: BARCODEGEN_URL not set, using mock client")
		barcodeClient = barcodegen.NewMockClient()
	}

	// AI Service: HTTPClient если AI_URL задан явно, иначе Mock (п.9 ТЗ).
	var aiClient ports.AIClient
	if url := os.Getenv(config.EnvAIURL); url != "" {
		log.Printf("worker ai: using HTTP client → %s", url)
		aiClient = aiadapter.NewHTTPClient(cfg.Services.AIURL, cfg.InternalServiceJWT).
			WithTimeouts(timeoutStore)
	} else {
		log.Printf("worker ai: AI_URL not set, using mock client")
		aiClient = aiadapter.NewMockClient()
	}

	// ── Kafka-продюсеры ───────────────────────────────────────────────────────

	// Events, Notifications, TransHistory: Kafka если KAFKA_BROKERS задан (п.10.3, п.11.2, п.11.3, п.14.4 ТЗ).
	// events.KafkaPublisher реализует все три порта: EventPublisher + NotificationsPublisher + TransHistoryPublisher.
	var eventPublisher ports.EventPublisher
	var notificationsPublisher ports.NotificationsPublisher
	var transHistoryPublisher ports.TransHistoryPublisher
	var kafkaProducer *kafkaadapter.Producer // сохраняем для Close() при shutdown
	if brokers := os.Getenv(config.EnvKafkaBrokers); brokers != "" {
		log.Printf("worker kafka: using real producer → brokers=%s", brokers)
		kafkaProducer = kafkaadapter.NewProducer(cfg.Kafka.Brokers)
		pub := events.NewKafkaPublisher(kafkaProducer)
		eventPublisher = pub
		notificationsPublisher = pub
		transHistoryPublisher = pub
	} else {
		log.Printf("worker kafka: KAFKA_BROKERS not set, using mock publishers")
		mock := events.NewMockPublisher()
		eventPublisher = mock
		notificationsPublisher = mock
		transHistoryPublisher = mock
	}

	// ── Use cases ─────────────────────────────────────────────────────────────

	quoteCase := usecase.NewQuoteUseCase(billingClient).
		WithPartialSuccessEnabled(cfg.Features.EnablePartialSuccess)
	chainExecutor := usecase.NewChainExecutor(barcodeClient, revisionStore)
	generateCase := usecase.NewGenerateUseCase(billingClient, barcodeClient, eventPublisher, quoteCase).
		WithPartialSuccessEnabled(cfg.Features.EnablePartialSuccess).
		WithChainExecutor(chainExecutor).
		WithRevisionStore(revisionStore).
		WithTransHistory(transHistoryPublisher).
		WithAI(aiClient)
	if cfg.Features.EnableNotifications {
		generateCase = generateCase.WithNotifications(notificationsPublisher)
	}

	handler := kafkatransport.NewBulkJobHandler(generateCase, eventPublisher)

	// Consumer: реальный Kafka если KAFKA_BROKERS задан, иначе MockConsumer (п.14.6 ТЗ).
	var consumer ports.BulkJobConsumer
	if brokers := os.Getenv(config.EnvKafkaBrokers); brokers != "" {
		log.Printf("worker bulk.tasks: using real Kafka consumer → brokers=%s group=%s",
			brokers, cfg.Kafka.GroupID)
		consumer = kafkaadapter.NewConsumer(cfg.Kafka.Brokers, cfg.Kafka.GroupID, handler.Handle)
	} else {
		log.Printf("worker bulk.tasks: KAFKA_BROKERS not set, using mock consumer")
		consumer = kafkaadapter.NewMockConsumer(handler.Handle, 256)
	}

	return &WorkerApp{
		consumer: consumer,
		producer: kafkaProducer,
	}
}
