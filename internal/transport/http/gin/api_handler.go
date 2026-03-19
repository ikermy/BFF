package gintransport

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
	"github.com/ikermy/BFF/internal/usecase"

	"github.com/gin-gonic/gin"
)

type APIHandler struct {
	quote          *usecase.QuoteUseCase
	generate       *usecase.GenerateUseCase
	edit           *usecase.EditUseCase
	consumer       ports.BulkJobConsumer
	revisionSchema *usecase.RevisionSchemaUseCase
	revisionStore  ports.RevisionConfigStore
	barcode        ports.BarcodeGenClient
	history        ports.HistoryClient
}

func NewAPIHandler(
	quote *usecase.QuoteUseCase,
	generate *usecase.GenerateUseCase,
	edit *usecase.EditUseCase,
	consumer ports.BulkJobConsumer,
	revisionSchema *usecase.RevisionSchemaUseCase,
	revisionStore ports.RevisionConfigStore,
	barcode ports.BarcodeGenClient,
	history ports.HistoryClient,
) *APIHandler {
	return &APIHandler{
		quote:          quote,
		generate:       generate,
		edit:           edit,
		consumer:       consumer,
		revisionSchema: revisionSchema,
		revisionStore:  revisionStore,
		barcode:        barcode,
		history:        history,
	}
}

// GetQuote — GET /api/v1/billing/quote (п.12.1 ТЗ).
func (h *APIHandler) GetQuote(c *gin.Context) {
	units, err := strconv.Atoi(c.Query("units"))
	if err != nil || units <= 0 {
		RespondError(c, domain.NewValidationError("units must be a positive integer"))
		return
	}

	revision := c.DefaultQuery("revision", "US_CA_08292017")
	userInfo, _ := GetUserInfo(c)

	result, err := h.quote.Execute(c.Request.Context(), userInfo.UserID, units, revision)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// Generate — POST /api/v1/barcode/generate (п.12.2 ТЗ).
func (h *APIHandler) Generate(c *gin.Context) {
	var req domain.GenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError(err.Error()))
		return
	}

	userInfo, _ := GetUserInfo(c)
	req.IdempotencyKey = c.GetHeader("X-Idempotency-Key")

	result, err := h.generate.Execute(c.Request.Context(), userInfo.UserID, req)
	if err != nil {
		// PARTIAL_FUNDS (п.15.1 ТЗ) — не ошибка, HTTP 200 с кодом в теле.
		var appErr *domain.AppError
		if errors.As(err, &appErr) && appErr.Code == domain.ErrCodePartialFunds {
			c.JSON(http.StatusOK, gin.H{
				"code":      domain.ErrCodePartialFunds,
				"message":   appErr.Message,
				"partial":   true,
				"confirmed": false,
			})
			return
		}
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// EditBarcode — POST /api/v1/barcode/:id/edit (п.10.1 ТЗ).
// Бесплатное редактирование: каждый пользователь имеет 1 право на изменение поля.
func (h *APIHandler) EditBarcode(c *gin.Context) {
	barcodeID := c.Param("id")
	if barcodeID == "" {
		RespondError(c, domain.NewValidationError("barcode id is required"))
		return
	}

	var req domain.EditRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError(err.Error()))
		return
	}

	userInfo, _ := GetUserInfo(c)
	req.IdempotencyKey = c.GetHeader("X-Idempotency-Key")
	result, err := h.edit.Execute(c.Request.Context(), userInfo.UserID, barcodeID, req)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetBarcode — GET /api/v1/barcode/:id (п.10.2 ТЗ).
// Возвращает поля существующего баркода для Remake (перегенерации).
// Фронтенд использует ответ для заполнения формы перед вызовом POST /barcode/generate.
func (h *APIHandler) GetBarcode(c *gin.Context) {
	barcodeID := c.Param("id")
	if barcodeID == "" {
		RespondError(c, domain.NewValidationError("barcode id is required"))
		return
	}

	record, err := h.history.GetBarcode(c.Request.Context(), barcodeID)
	if err != nil {
		RespondError(c, domain.NewBarcodeGenError(err))
		return
	}
	c.JSON(http.StatusOK, record)
}

// GetRevisionSchema — GET /api/v1/revisions/:revision/schema (п.14.5 ТЗ).
func (h *APIHandler) GetRevisionSchema(c *gin.Context) {
	revision := c.Param("revision")
	schema, err := h.revisionSchema.Execute(c.Request.Context(), revision)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, schema)
}

// ListRevisions — GET /api/v1/revisions (список доступных ревизий для фронтенда).
func (h *APIHandler) ListRevisions(c *gin.Context) {
	configs, err := h.revisionStore.ListConfigs(c.Request.Context())
	if err != nil {
		RespondError(c, err)
		return
	}

	type revisionItem struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Enabled     bool   `json:"enabled"`
	}
	items := make([]revisionItem, 0, len(configs))
	for _, cfg := range configs {
		if cfg.Enabled {
			items = append(items, revisionItem{
				Name:        cfg.Name,
				DisplayName: cfg.DisplayName,
				Enabled:     cfg.Enabled,
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"revisions": items})
}

// BulkWake — POST /api/v1/bulk/wake (п.14.6 ТЗ).
// Вызывается Bulk Service (service-token) для проверки что BFF читает Kafka.
func (h *APIHandler) BulkWake(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":          "awake",
		"pendingMessages": h.consumer.PendingCount(),
	})
}

// GeneratePDF417 — POST /api/v1/barcode/generate/pdf417 (п.12.3 ТЗ).
// Dedicated эндпоинт для генерации PDF417 с опциями рендеринга.
// Не проходит через billing — прямой вызов BarcodeGen.
// X-Idempotency-Key из заголовка форвардируется в BarcodeGen (п.8.2 ТЗ).
func (h *APIHandler) GeneratePDF417(c *gin.Context) {
	var req domain.GeneratePDF417Request
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError(err.Error()))
		return
	}
	if len(req.Fields) == 0 {
		RespondError(c, domain.NewValidationError("fields are required"))
		return
	}
	// Инжектируем idempotency key для форвардинга в BarcodeGen (п.8.2 ТЗ)
	req.IdempotencyKey = c.GetHeader("X-Idempotency-Key")

	resp, err := h.barcode.GeneratePDF417(c.Request.Context(), req)
	if err != nil {
		RespondError(c, domain.NewBarcodeGenError(err))
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GenerateCode128 — POST /api/v1/barcode/generate/code128 (п.12.4 ТЗ).
// Dedicated эндпоинт для генерации Code128 с опциями рендеринга.
// Не проходит через billing — прямой вызов BarcodeGen.
// X-Idempotency-Key из заголовка форвардируется в BarcodeGen (п.8.2 ТЗ).
func (h *APIHandler) GenerateCode128(c *gin.Context) {
	var req domain.GenerateCode128Request
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError(err.Error()))
		return
	}
	// Инжектируем idempotency key для форвардинга в BarcodeGen (п.8.2 ТЗ)
	req.IdempotencyKey = c.GetHeader("X-Idempotency-Key")

	resp, err := h.barcode.GenerateCode128(c.Request.Context(), req)
	if err != nil {
		RespondError(c, domain.NewBarcodeGenError(err))
		return
	}
	c.JSON(http.StatusOK, resp)
}
