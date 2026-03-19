package ports

import (
	"context"

	"github.com/ikermy/BFF/internal/domain"
)

// BillingClient — порт для работы с Billing Service (п.7, п.14.4 ТЗ).
type BillingClient interface {
	Quote(ctx context.Context, userID string, units int, revision string) (domain.QuoteResult, error)
	// Block блокирует средства перед генерацией.
	// req.BySource содержит точную разбивку из quote (Split Payment, п.7.1 ТЗ).
	// Для бесплатного редактирования (п.10.1 ТЗ) req.Units=0, BySource — нулевой.
	Block(ctx context.Context, req domain.BlockRequest) error
	BlockBatch(ctx context.Context, userID string, count int, batchID string) ([]string, error)
	// Capture списывает точное количество успешно сгенерированных единиц (п.14.4 ТЗ).
	Capture(ctx context.Context, sagaID string, units int) error
	// Release разблокирует неиспользованные единицы (п.14.4 ТЗ).
	Release(ctx context.Context, sagaID string, units int) error
}

// BarcodeGenClient — порт для работы с BarcodeGen Service (п.8 ТЗ).
// Dedicated endpoints: GeneratePDF417 (п.12.3) и GenerateCode128 (п.12.4).
// Общего /api/v1/generate в ТЗ нет — только форматные эндпоинты.
type BarcodeGenClient interface {
	// Calculate — рассчитывает значение поля через BarcodeGen (п.8.2 ТЗ, source=calculate).
	Calculate(ctx context.Context, revision, field string, knownFields map[string]any) (any, error)
	// Random — генерирует случайное значение поля через BarcodeGen (п.8.2 ТЗ, source=random).
	Random(ctx context.Context, revision, field string, params map[string]any) (any, error)
	// GeneratePDF417 — dedicated генерация PDF417 с опциями рендеринга (п.12.3 ТЗ).
	GeneratePDF417(ctx context.Context, req domain.GeneratePDF417Request) (domain.GeneratePDF417Response, error)
	// GenerateCode128 — dedicated генерация Code128 с опциями рендеринга (п.12.4 ТЗ).
	GenerateCode128(ctx context.Context, req domain.GenerateCode128Request) (domain.GenerateCode128Response, error)
}

// EventPublisher — порт публикации Kafka-событий (п.10.3, п.14.4, п.8.3 Bulk_Service_TZ).
type EventPublisher interface {
	PublishSagaCompleted(ctx context.Context, sagaID string) error
	PublishBulkResult(ctx context.Context, event domain.BulkResultEvent) error
	// PublishPartialCompleted публикует billing.saga.partial_completed (п.14.4 ТЗ).
	PublishPartialCompleted(ctx context.Context, event domain.PartialCompletedEvent) error
	// PublishBarcodeEdited публикует barcode.edited после бесплатного редактирования (п.10.1 ТЗ).
	PublishBarcodeEdited(ctx context.Context, event domain.BarcodeEditedEvent) error
	// PublishBarcodeGenerated публикует barcode.generated после успешной генерации (п.10.3 ТЗ).
	PublishBarcodeGenerated(ctx context.Context, event domain.BarcodeGeneratedEvent) error
}

// AIClient — порт для работы с AI Service (п.9 ТЗ).
// Вызывается при generateSignature=true или generatePhoto=true в GenerateRequest.
type AIClient interface {
	// GenerateSignature генерирует подпись для пользователя (п.9.2 ТЗ).
	// Эндпоинт: POST /internal/ai/signature с Service Token.
	GenerateSignature(ctx context.Context, req domain.AISignatureRequest) (domain.AISignatureResponse, error)
	// GeneratePhoto генерирует фото пользователя (п.9.2 ТЗ).
	// Эндпоинт: POST /internal/ai/photo.
	GeneratePhoto(ctx context.Context, req domain.AIPhotoRequest) (domain.AIPhotoResponse, error)
}

// AuthClient — порт валидации User JWT через Legacy Auth Service (п.11.1, п.16.1 ТЗ).
type AuthClient interface {
	// ValidateToken валидирует JWT-токен и возвращает базовые данные пользователя.
	// Используется в UserJWTMiddleware для HTTP-запросов.
	ValidateToken(ctx context.Context, token string) (domain.UserInfo, error)

	// GetUserInfo возвращает полную информацию о пользователе по его ID (п.11.1 ТЗ).
	// Необходим когда BFF имеет только userID без JWT-токена (например, из Kafka-сообщений
	// bulk.tasks, где передаётся msg.UserID, а не Bearer-токен).
	// В текущих HTTP-flows не вызывается — ValidateToken уже возвращает domain.UserInfo.
	GetUserInfo(ctx context.Context, userID string) (domain.UserInfo, error)
}

// NotificationsPublisher — порт отправки уведомлений через Kafka (п.11.2 ТЗ).
// Топик: notifications.send. Направление: BFF → Notifications Service.
type NotificationsPublisher interface {
	// SendGenerationComplete публикует уведомление об успешной генерации баркодов.
	SendGenerationComplete(ctx context.Context, req domain.NotificationRequest) error
	// SendGenerationError публикует уведомление об ошибке генерации.
	SendGenerationError(ctx context.Context, req domain.ErrorNotificationRequest) error
}

// TransHistoryPublisher — порт логирования транзакций через Kafka (п.11.3 ТЗ).
// Топик: trans-history.log. Направление: BFF → TransHistory Service.
type TransHistoryPublisher interface {
	// LogTransaction записывает транзакцию в историю.
	// Type: GENERATION | PAYMENT | REFUND.
	LogTransaction(ctx context.Context, log domain.TransactionLog) error
}

// HistoryClient — порт для работы с History Service (п.10.1, п.10.2 ТЗ).
type HistoryClient interface {
	// CheckFreeEdit проверяет, доступно ли бесплатное редактирование для баркода (п.10.1 ТЗ).
	// GET /internal/barcode/:id/check-free-edit
	CheckFreeEdit(ctx context.Context, barcodeID string) (canEdit bool, err error)
	// GetBarcode возвращает данные существующего баркода для Remake (п.10.2 ТЗ).
	// Фронтенд использует ответ для заполнения формы перед перегенерацией.
	GetBarcode(ctx context.Context, barcodeID string) (domain.BarcodeRecord, error)
}

// TopUpBonusStore — порт хранения конфига бонусов за пополнение (п.14.7 ТЗ).
type TopUpBonusStore interface {
	Get(ctx context.Context) (domain.TopupBonusConfig, error) // TODO скорее всего не ясное описание в 14.7 ТЗ без метода GET
	Set(ctx context.Context, cfg domain.TopupBonusConfig) error
}

// BulkJobConsumer — порт Kafka-консьюмера топика bulk.tasks (п.14.6 ТЗ).
// Start запускает чтение сообщений в фоне до отмены ctx.
// PendingCount возвращает количество необработанных сообщений в буфере.
type BulkJobConsumer interface {
	Start(ctx context.Context) error
	PendingCount() int
}

// RevisionSchemaStore — порт получения схемы формы для ревизии (п.14.5 ТЗ).
type RevisionSchemaStore interface {
	GetSchema(ctx context.Context, revision string) (domain.RevisionSchema, error)
}

// RevisionConfigStore — порт admin-управления конфигурацией ревизий (п.13.1 ТЗ).
// Отличается от RevisionSchemaStore: здесь enabled + calculationChain, не форма.
type RevisionConfigStore interface {
	ListConfigs(ctx context.Context) ([]domain.RevisionConfig, error)
	GetConfig(ctx context.Context, name string) (domain.RevisionConfig, error)
	UpdateConfig(ctx context.Context, name string, req domain.UpdateRevisionRequest) error
}

// KafkaTopicStore — порт управления реестром Kafka топиков (п.13.3 ТЗ).
type KafkaTopicStore interface {
	List(ctx context.Context) ([]domain.KafkaTopic, error)
}

// TimeoutStore — порт хранения конфига таймаутов сервисов (п.13.2 ТЗ).
type TimeoutStore interface {
	Get(ctx context.Context) (domain.ServiceTimeouts, error)
	Set(ctx context.Context, t domain.ServiceTimeouts) error
}

// IdempotencyStore — порт хранилища идемпотентности (п.14.1 ТЗ).
// В production заменяется на Redis-реализацию (REDIS_URL).
type IdempotencyStore interface {
	// Get возвращает (body, true, nil) если ключ уже завершён с готовым ответом.
	// Возвращает (nil, false, nil) если ключ не найден или ещё in-flight.
	Get(ctx context.Context, key string) ([]byte, bool, error)
	// Reserve резервирует ключ (SetNX) до начала выполнения — in-flight маркер.
	// Возвращает true если ключ успешно зарезервирован (первый запрос).
	// Возвращает false если ключ уже существует (параллельный или повторный запрос).
	Reserve(ctx context.Context, key string) (bool, error)
	// Set сохраняет готовый ответ под ключом (перезаписывает in-flight маркер).
	Set(ctx context.Context, key string, body []byte) error
}
