package app

import (
	"context"
	"log"
	"os"
	"time"

	aiadapter "github.com/ikermy/BFF/internal/adapters/ai"
	"github.com/ikermy/BFF/internal/adapters/auth"
	"github.com/ikermy/BFF/internal/adapters/barcodegen"
	"github.com/ikermy/BFF/internal/adapters/billing"
	"github.com/ikermy/BFF/internal/adapters/events"
	"github.com/ikermy/BFF/internal/adapters/history"
	"github.com/ikermy/BFF/internal/adapters/idempotency"
	kafkaadapter "github.com/ikermy/BFF/internal/adapters/kafka"
	"github.com/ikermy/BFF/internal/adapters/revisions"
	"github.com/ikermy/BFF/internal/adapters/timeouts"
	"github.com/ikermy/BFF/internal/adapters/topupbonus"
	"github.com/ikermy/BFF/internal/config"
	"github.com/ikermy/BFF/internal/ports"
	gintransport "github.com/ikermy/BFF/internal/transport/http/gin"
	kafkatransport "github.com/ikermy/BFF/internal/transport/kafka"
	"github.com/ikermy/BFF/internal/usecase"
)

type APIApp struct {
	server   *gintransport.Server
	producer *kafkaadapter.Producer // nil если используется mock
}

func (a *APIApp) Run(ctx context.Context) error {
	return a.server.Run(ctx)
}

// Close освобождает ресурсы приложения: закрывает Kafka Writer.
// Вызывается после Run() при graceful shutdown.
func (a *APIApp) Close() {
	if a.producer != nil {
		if err := a.producer.Close(); err != nil {
			log.Printf("warn: kafka producer close: %v", err)
		}
	}
}

func BuildAPIApp(cfg config.Config) *APIApp {
	// ── Инфраструктура (stores) — объявляются первыми, т.к. используются HTTP-клиентами ──
	topUpBonusStore := topupbonus.NewMemoryStore()
	kafkaTopicsStore := kafkaadapter.NewTopicStore()
	// timeoutStore инициализируется из ENV; HTTP-клиенты читают актуальный таймаут динамически (п.13.2 ТЗ).
	timeoutStore := timeouts.NewMemoryStore(cfg.Timeouts.BarcodeGen, cfg.Timeouts.Billing, cfg.Timeouts.AI, 5*time.Second, 5*time.Second)
	revisionStore := revisions.NewMemoryStore()
	if err := topUpBonusStore.LoadFromFile(config.TopupBonusConfigPath); err != nil {
		log.Printf("warn: cannot load topup-bonus config from %s: %v (using defaults)", config.TopupBonusConfigPath, err)
	}
	if err := timeoutStore.LoadFromFile(config.TimeoutsConfigPath); err != nil {
		log.Printf("warn: cannot load timeouts config from %s: %v (using defaults)", config.TimeoutsConfigPath, err)
	}

	// Idempotency: RedisStore если REDIS_URL задан явно, иначе MemoryStore (п.14.1 ТЗ).
	var idempotencyStore ports.IdempotencyStore
	if url := os.Getenv(config.EnvRedisURL); url != "" {
		log.Printf("idempotency: using Redis → %s", url)
		redisStore, err := idempotency.NewRedisStore(cfg.Redis.URL, cfg.Idempotency.TTL)
		if err != nil {
			log.Fatalf("idempotency: redis init failed: %v", err)
		}
		idempotencyStore = redisStore
	} else {
		log.Printf("idempotency: REDIS_URL not set, using in-memory store")
		idempotencyStore = idempotency.NewMemoryStore(cfg.Idempotency.TTL)
	}

	// ── HTTP-клиенты downstream-сервисов ─────────────────────────────────────────────────

	// Auth: HTTPClient если AUTH_URL задан явно, иначе Mock (п.11.1 ТЗ).
	var authClient ports.AuthClient
	if url := os.Getenv(config.EnvAuthURL); url != "" {
		log.Printf("auth: using HTTP client → %s", url)
		authClient = auth.NewHTTPClient(cfg.Services.AuthURL).
			WithTimeouts(timeoutStore) // динамический таймаут
	} else {
		log.Printf("auth: AUTH_URL not set, using mock client")
		authClient = auth.NewMockClient()
	}

	// Billing: HTTPClient если BILLING_URL задан явно, иначе Mock (п.7 ТЗ).
	var billingClient ports.BillingClient
	if url := os.Getenv(config.EnvBillingURL); url != "" {
		log.Printf("billing: using HTTP client → %s", url)
		billingClient = billing.NewHTTPClient(cfg.Services.BillingURL).
			WithTimeouts(timeoutStore) // п.13.2: динамический таймаут
	} else {
		log.Printf("billing: BILLING_URL not set, using mock client")
		billingClient = billing.NewMockClient(cfg.UnitPrice)
	}

	// BarcodeGen: HTTPClient если BARCODEGEN_URL задан явно, иначе Mock (п.8 ТЗ).
	var barcodeClient ports.BarcodeGenClient
	if url := os.Getenv(config.EnvBarcodeGenURL); url != "" {
		log.Printf("barcodegen: using HTTP client → %s", url)
		barcodeClient = barcodegen.NewHTTPClient(cfg.Services.BarcodeGenURL, cfg.Timeouts.BarcodeGen).
			WithTimeouts(timeoutStore) // п.13.2: динамический таймаут
	} else {
		log.Printf("barcodegen: BARCODEGEN_URL not set, using mock client")
		barcodeClient = barcodegen.NewMockClient()
	}

	// AI Service: HTTPClient если AI_URL задан явно, иначе Mock (п.9 ТЗ).
	var aiClient ports.AIClient
	if url := os.Getenv(config.EnvAIURL); url != "" {
		log.Printf("ai: using HTTP client → %s", url)
		aiClient = aiadapter.NewHTTPClient(cfg.Services.AIURL, cfg.InternalServiceJWT).
			WithTimeouts(timeoutStore) // п.13.2: photo timeout динамический
	} else {
		log.Printf("ai: AI_URL not set, using mock client")
		aiClient = aiadapter.NewMockClient()
	}

	// History: HTTPClient если HISTORY_URL задан явно, иначе Mock (п.10 ТЗ).
	var historyClient ports.HistoryClient
	if url := os.Getenv(config.EnvHistoryURL); url != "" {
		log.Printf("history: using HTTP client → %s", url)
		historyClient = history.NewHTTPClient(cfg.Services.HistoryURL).
			WithTimeouts(timeoutStore) // динамический таймаут
	} else {
		log.Printf("history: HISTORY_URL not set, using mock client")
		historyClient = history.NewMockClient()
	}

	// ── Kafka-продюсеры ───────────────────────────────────────────────────────────────────

	// Events, Notifications, TransHistory: Kafka если KAFKA_BROKERS задан (п.10.3, п.11.2, п.11.3, п.14.4 ТЗ).
	// events.KafkaPublisher реализует все три порта: EventPublisher + NotificationsPublisher + TransHistoryPublisher.
	var eventPublisher ports.EventPublisher
	var notificationsPublisher ports.NotificationsPublisher
	var transHistoryPublisher ports.TransHistoryPublisher
	var kafkaProducer *kafkaadapter.Producer // сохраняем для Close() при shutdown
	if brokers := os.Getenv(config.EnvKafkaBrokers); brokers != "" {
		log.Printf("kafka: using real producer → brokers=%s", brokers)
		kafkaProducer = kafkaadapter.NewProducer(cfg.Kafka.Brokers)
		pub := events.NewKafkaPublisher(kafkaProducer)
		eventPublisher = pub
		notificationsPublisher = pub
		transHistoryPublisher = pub
	} else {
		log.Printf("kafka: KAFKA_BROKERS not set, using mock publishers")
		mock := events.NewMockPublisher()
		eventPublisher = mock
		notificationsPublisher = mock
		transHistoryPublisher = mock
	}

	// ── Загрузка конфигурации ─────────────────────────────────────────────────────────────

	if err := revisionStore.LoadFromDir(config.RevisionsDirPath); err != nil {
		log.Printf("warn: cannot load revisions from yaml: %v (using defaults)", err)
	}

	// ── Use cases ─────────────────────────────────────────────────────────────────────────

	quoteCase := usecase.NewQuoteUseCase(billingClient).
		WithPartialSuccessEnabled(cfg.Features.EnablePartialSuccess)
	bulkCase := usecase.NewBulkUseCase(billingClient)
	chainExecutor := usecase.NewChainExecutor(barcodeClient, revisionStore)
	generateCase := usecase.NewGenerateUseCase(billingClient, barcodeClient, eventPublisher, quoteCase).
		WithPartialSuccessEnabled(cfg.Features.EnablePartialSuccess).
		WithChainExecutor(chainExecutor).
		WithTransHistory(transHistoryPublisher).
		WithAI(aiClient).
		WithRevisionStore(revisionStore)
	if cfg.Features.EnableNotifications {
		generateCase = generateCase.WithNotifications(notificationsPublisher)
	}
	editCase := usecase.NewEditUseCase(billingClient, barcodeClient, historyClient, eventPublisher)
	revisionSchemaCase := usecase.NewRevisionSchemaUseCase(revisionStore)

	bulkHandler := kafkatransport.NewBulkJobHandler(generateCase, eventPublisher)

	// Consumer: реальный Kafka если KAFKA_BROKERS задан, иначе MockConsumer (п.14.6 ТЗ).
	// BulkWake endpoint использует consumer.PendingCount() для мониторинга.
	var bulkConsumer ports.BulkJobConsumer
	if brokers := os.Getenv(config.EnvKafkaBrokers); brokers != "" {
		log.Printf("api bulk.tasks: using real Kafka consumer → brokers=%s group=%s",
			brokers, cfg.Kafka.GroupID)
		bulkConsumer = kafkaadapter.NewConsumer(cfg.Kafka.Brokers, cfg.Kafka.GroupID, bulkHandler.Handle)
	} else {
		log.Printf("api bulk.tasks: KAFKA_BROKERS not set, using mock consumer")
		bulkConsumer = kafkaadapter.NewMockConsumer(bulkHandler.Handle, 256)
	}

	apiHandler := gintransport.NewAPIHandler(quoteCase, generateCase, editCase, bulkConsumer, revisionSchemaCase, revisionStore, barcodeClient, historyClient)
	internalHandler := gintransport.NewInternalHandler(quoteCase, bulkCase)
	adminHandler := gintransport.NewAdminHandler(topUpBonusStore, kafkaTopicsStore, timeoutStore, revisionStore)
	router := gintransport.NewRouter(
		gintransport.Handlers{API: apiHandler, Internal: internalHandler, Admin: adminHandler},
		authClient,
		idempotencyStore,
		cfg.InternalServiceJWT,
		cfg.AdminJWT,
		cfg.Features.EnableLegacyAuth,
		cfg.Features.EnableIdempotency,
		cfg.MaintenanceMode,
	)

	return &APIApp{
		server:   gintransport.NewServer(cfg.Port, router),
		producer: kafkaProducer,
	}
}
