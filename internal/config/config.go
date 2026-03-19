package config

import (
	"os"
	"strconv"
	"time"
)

// Имена основных переменных окружения и app-флагов, используемых BFF.
// Константы защищают от опечаток и позволяют находить все точки использования.
const (
	// BFF
	EnvPort               = "BFF_PORT"
	EnvInternalServiceJWT = "INTERNAL_SERVICE_JWT"
	EnvAdminJWT           = "ADMIN_JWT"

	// Downstream services
	EnvBarcodeGenURL = "BARCODEGEN_URL"
	EnvBillingURL    = "BILLING_URL"
	EnvAIURL         = "AI_URL"
	EnvHistoryURL    = "HISTORY_URL"

	// Legacy services (п.17.1 ТЗ)
	// AUTH_URL используется для HTTP-клиента (п.11.1 ТЗ: validateToken, getUserInfo).
	// NOTIFICATIONS_URL и TRANS_HISTORY_URL перечислены в п.17.1 ТЗ как обязательные ENV vars,
	// однако BFF взаимодействует с этими сервисами через Kafka (п.11.2, п.11.3 ТЗ), а не HTTP.
	// URL хранятся в конфиге для соответствия спецификации и возможного future use.
	EnvAuthURL          = "AUTH_URL"
	EnvNotificationsURL = "NOTIFICATIONS_URL"
	EnvTransHistoryURL  = "TRANS_HISTORY_URL"

	// Kafka
	EnvKafkaBrokers = "KAFKA_BROKERS"
	EnvKafkaGroupID = "KAFKA_GROUP_ID" // consumer group для bulk.tasks (п.14.6 ТЗ)

	// Redis
	EnvRedisURL = "REDIS_URL"

	// Timeouts
	EnvBarcodeGenTimeout = "BARCODEGEN_TIMEOUT"
	EnvBillingTimeout    = "BILLING_TIMEOUT"
	EnvAITimeout         = "AI_TIMEOUT"

	// Idempotency
	EnvIdempotencyTTL = "IDEMPOTENCY_TTL"

	// Billing
	EnvUnitPrice = "UNIT_PRICE"

	// Feature flags (п.15 ТЗ)
	EnvEnablePartialSuccess = "ENABLE_PARTIAL_SUCCESS"
	EnvEnableIdempotency    = "ENABLE_IDEMPOTENCY"
	EnvEnableLegacyAuth     = "ENABLE_LEGACY_AUTH"
	EnvEnableNotifications  = "ENABLE_NOTIFICATIONS"

	// Rollback (п.19 ТЗ)
	EnvMaintenanceMode = "MAINTENANCE_MODE"
)

// Services — URL-адреса downstream и legacy сервисов (п.17.1 ТЗ).
// NotificationsURL и TransHistoryURL объявлены согласно п.17.1 ТЗ.
// BFF не создаёт HTTP-клиентов для этих сервисов — взаимодействие идёт через Kafka
// (п.11.2: notifications.send, п.11.3: trans-history.log).
type Services struct {
	BarcodeGenURL    string
	BillingURL       string
	AIURL            string
	HistoryURL       string
	AuthURL          string
	NotificationsURL string // п.17.1 ТЗ; Kafka-топик: notifications.send
	TransHistoryURL  string // п.17.1 ТЗ; Kafka-топик: trans-history.log
}

// Kafka — настройки Kafka.
type Kafka struct {
	Brokers string
	GroupID string // KAFKA_GROUP_ID; дефолт: "bff-bulk-worker"
}

// Redis — настройки Redis.
type Redis struct {
	URL string
}

// Timeouts — таймауты вызовов к сервисам (п.15 ТЗ).
// History и Auth не выносятся в ENV — их дефолты заданы константами в пакетах клиентов.
type Timeouts struct {
	BarcodeGen time.Duration
	Billing    time.Duration
	AI         time.Duration
}

// Idempotency — настройки идемпотентности.
type Idempotency struct {
	TTL time.Duration
}

// FeatureFlags — флаги включения/выключения функций (п.15 ТЗ).
type FeatureFlags struct {
	EnablePartialSuccess bool // ENABLE_PARTIAL_SUCCESS (default: true)
	EnableIdempotency    bool // ENABLE_IDEMPOTENCY (default: true)
	EnableLegacyAuth     bool // ENABLE_LEGACY_AUTH (default: true)
	EnableNotifications  bool // ENABLE_NOTIFICATIONS (default: true)
}

// Config — runtime-конфигурация BFF из ENV и флагов приложения.
type Config struct {
	Port               string
	InternalServiceJWT string
	AdminJWT           string
	UnitPrice          float64
	MaintenanceMode    bool
	Services           Services
	Kafka              Kafka
	Redis              Redis
	Timeouts           Timeouts
	Idempotency        Idempotency
	Features           FeatureFlags
}

func Load() Config {
	return Config{
		Port:               getEnv(EnvPort, "8080"),
		InternalServiceJWT: getEnv(EnvInternalServiceJWT, "dev-internal-token"),
		AdminJWT:           getEnv(EnvAdminJWT, "dev-admin-token"),
		UnitPrice:          getEnvFloat(EnvUnitPrice, 0.50),
		MaintenanceMode:    getEnvBool(EnvMaintenanceMode, false),

		Services: Services{
			BarcodeGenURL:    getEnv(EnvBarcodeGenURL, "http://barcodegen:8080"),
			BillingURL:       getEnv(EnvBillingURL, "http://billing:3000"),
			AIURL:            getEnv(EnvAIURL, "http://ai-service:8080"),
			HistoryURL:       getEnv(EnvHistoryURL, "http://history:3000"),
			AuthURL:          getEnv(EnvAuthURL, "http://auth-service:3000"),
			NotificationsURL: getEnv(EnvNotificationsURL, "http://notifications:3000"),
			TransHistoryURL:  getEnv(EnvTransHistoryURL, "http://trans-history:3000"),
		},

		Kafka: Kafka{
			Brokers: getEnv(EnvKafkaBrokers, "kafka:9092"),
			GroupID: getEnv(EnvKafkaGroupID, "bff-bulk-worker"),
		},

		Redis: Redis{
			URL: getEnv(EnvRedisURL, "redis://redis:6379/0"),
		},

		Timeouts: Timeouts{
			BarcodeGen: getEnvDuration(EnvBarcodeGenTimeout, 30*time.Second),
			Billing:    getEnvDuration(EnvBillingTimeout, 5*time.Second),
			AI:         getEnvDuration(EnvAITimeout, 60*time.Second),
		},

		Idempotency: Idempotency{
			TTL: getEnvSecondsDuration(EnvIdempotencyTTL, 24*time.Hour),
		},

		Features: FeatureFlags{
			EnablePartialSuccess: getEnvBool(EnvEnablePartialSuccess, true),
			EnableIdempotency:    getEnvBool(EnvEnableIdempotency, true),
			EnableLegacyAuth:     getEnvBool(EnvEnableLegacyAuth, true),
			EnableNotifications:  getEnvBool(EnvEnableNotifications, true),
		},
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// getEnvDuration читает значение в миллисекундах (как в ТЗ: BARCODEGEN_TIMEOUT=30000)
// и конвертирует в time.Duration.
func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	ms, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}

// getEnvSecondsDuration читает значение в секундах.
// Согласно ТЗ: IDEMPOTENCY_TTL=86400 (24 часа).
func getEnvSecondsDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
