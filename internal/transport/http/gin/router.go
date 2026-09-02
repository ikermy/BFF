package gintransport

import (
	"net/http"

	"github.com/ikermy/BFF/internal/ports"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Handlers struct {
	API      *APIHandler
	Internal *InternalHandler
	Admin    *AdminHandler
	Auth     *AuthHandler
}

// NewRouter собирает Gin-роутер с тремя защищёнными группами (п.16.1 ТЗ):
//   - /api/v1  — User JWT (через Auth Service)
//   - /admin   — Admin JWT (role=admin)
//   - /internal — Service Token
func NewRouter(
	h Handlers,
	auth ports.AuthClient,
	idempotency ports.IdempotencyStore,
	internalJWT string,
	adminJWT string,
	enableLegacyAuth bool,
	enableIdempotency bool,
	maintenanceMode bool,
) *gin.Engine {
	r := gin.New()
	r.Use(MaintenanceModeMiddleware(maintenanceMode))
	r.Use(MetricsMiddleware())
	r.Use(gin.Recovery())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// /api/v1/auth/* — публичные auth-операции (login/register), проксируются в Auth Service.
	// Не требуют User JWT: вызываются фронтендом до получения токена.
	if h.Auth != nil {
		authGroup := r.Group("/api/v1/auth")
		{
			authGroup.POST("/login", h.Auth.Login)
			authGroup.POST("/register", h.Auth.Register)
			authGroup.POST("/telegram", h.Auth.TelegramAuth)
		}
	}

	// /api/v1/* — защищён User JWT через Legacy Auth Service
	api := r.Group("/api/v1")
	api.Use(UserJWTMiddleware(auth, enableLegacyAuth))
	{
		api.GET("/billing/quote", h.API.GetQuote)
		api.GET("/revisions", h.API.ListRevisions)
		api.GET("/revisions/:revision/schema", h.API.GetRevisionSchema)

		// Профиль пользователя (email, username, nickname, фото, telegram) → Auth Service
		api.GET("/settings/profile", h.API.GetUserProfile)

		// Обновление фотографии профиля (base64 data URL) → Auth Service
		api.POST("/settings/avatar", h.API.ChangeAvatar)

		// Обновление Telegram username профиля → Auth Service
		api.POST("/settings/telegram", h.API.ChangeTelegramUsername)

		// Обновление отображаемого имени (nickname) профиля → Auth Service
		api.POST("/settings/nickname", h.API.ChangeNickname)

		// Привязка email к аккаунту (в т.ч. для telegram-аккаунтов) → Auth Service
		api.POST("/settings/email", h.API.LinkEmail)

		// Смена Telegram identity пользователя (виджет) → Auth Service
		api.POST("/settings/telegram-account", h.API.ChangeTelegramAccount)

		// Смена пароля пользователя → Auth Service
		api.POST("/settings/password", h.API.ChangePassword)

		// POST /barcode/generate — требует X-Idempotency-Key (п.14.1 ТЗ)
		api.POST("/barcode/generate",
			idempotencyKeyRequired(enableIdempotency),
			IdempotencyMiddleware(idempotency, enableIdempotency),
			h.API.Generate,
		)

		// Dedicated форматные эндпоинты (п.12.3, 12.4 ТЗ) — без billing, прямой вызов BarcodeGen.
		// Также требуют X-Idempotency-Key (п.14.1 ТЗ: все write-операции).
		// Ключ дополнительно форвардируется в BarcodeGen как X-Idempotency-Key (п.8.2 ТЗ).
		api.POST("/barcode/generate/pdf417",
			idempotencyKeyRequired(enableIdempotency),
			IdempotencyMiddleware(idempotency, enableIdempotency),
			h.API.GeneratePDF417,
		)
		api.POST("/barcode/generate/code128",
			idempotencyKeyRequired(enableIdempotency),
			IdempotencyMiddleware(idempotency, enableIdempotency),
			h.API.GenerateCode128,
		)

		// GET /barcode/:id — получить данные баркода для Remake (п.10.2 ТЗ)
		api.GET("/barcode/:id", h.API.GetBarcode)
		// POST /barcode/:id/edit — бесплатное редактирование (п.10.1 ТЗ)
		api.POST("/barcode/:id/edit",
			idempotencyKeyRequired(enableIdempotency),
			IdempotencyMiddleware(idempotency, enableIdempotency),
			h.API.EditBarcode,
		)
	}

	// /api/v1/bulk/wake — service-token (вызывается Bulk Service, п.14.6 ТЗ).
	bulkWake := r.Group("/api/v1/bulk")
	bulkWake.Use(ServiceJWTMiddleware(internalJWT))
	{
		bulkWake.POST("/wake", h.API.BulkWake)
	}

	// /admin/* — защищён Admin JWT
	admin := r.Group("/admin")
	admin.Use(AdminJWTMiddleware(adminJWT))
	{
		admin.GET("/revisions", h.Admin.ListRevisions)
		admin.PUT("/revisions/:revision",
			idempotencyKeyRequired(enableIdempotency),
			IdempotencyMiddleware(idempotency, enableIdempotency),
			h.Admin.UpdateRevision,
		)
		admin.PUT("/config/topup-bonus",
			idempotencyKeyRequired(enableIdempotency),
			IdempotencyMiddleware(idempotency, enableIdempotency),
			h.Admin.UpdateTopUpBonus,
		)
		admin.PUT("/config/timeouts",
			idempotencyKeyRequired(enableIdempotency),
			IdempotencyMiddleware(idempotency, enableIdempotency),
			h.Admin.UpdateTimeouts,
		)
		admin.GET("/kafka/topics", h.Admin.ListKafkaTopics)
	}

	// /internal/* — защищён Service Token
	internal := r.Group("/internal")
	internal.Use(ServiceJWTMiddleware(internalJWT))
	{
		internal.POST("/validate", h.Internal.Validate)
		internal.POST("/billing/quote", h.Internal.Quote)
		internal.POST("/billing/block-batch",
			idempotencyKeyRequired(enableIdempotency),
			IdempotencyMiddleware(idempotency, enableIdempotency),
			h.Internal.BlockBatch,
		)
		// capabilities уточнения ТЗ §4: эндпоинты для Verification Service
		internal.GET("/revisions", h.Internal.ListRevisions)
		internal.GET("/revisions/:revision/schema", h.Internal.GetRevisionSchema)
		internal.POST("/v1/capabilities/generate-raw", h.Internal.GenerateRaw)
	}

	// D1 (отчёт §4.3): billing-stub для встроенного биллинга BarcodeGen.
	// Регистрируется ВНЕ защищённой /internal-группы: BarcodeGen присылает сюда
	// пользовательский JWT (canBuy(data.token)), а не сервисный токен BFF.
	r.GET("/internal/v1/billing/check/credits", h.Internal.CheckCreditsStub)

	return r
}
