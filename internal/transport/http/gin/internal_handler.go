package gintransport

import (
	"net/http"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
	"github.com/ikermy/BFF/internal/usecase"

	"github.com/gin-gonic/gin"
)

type InternalHandler struct {
	quote          *usecase.QuoteUseCase
	bulk           *usecase.BulkUseCase
	revisionStore  ports.RevisionConfigStore
	revisionSchema *usecase.RevisionSchemaUseCase
	barcode        ports.BarcodeGenClient
}

func NewInternalHandler(
	quote *usecase.QuoteUseCase,
	bulk *usecase.BulkUseCase,
	revisionStore ports.RevisionConfigStore,
	revisionSchema *usecase.RevisionSchemaUseCase,
	barcode ports.BarcodeGenClient,
) *InternalHandler {
	return &InternalHandler{
		quote:          quote,
		bulk:           bulk,
		revisionStore:  revisionStore,
		revisionSchema: revisionSchema,
		barcode:        barcode,
	}
}

// Validate — POST /internal/validate (п.7.1 Bulk_Service_TZ).
func (h *InternalHandler) Validate(c *gin.Context) {
	var req domain.InternalValidateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, h.bulk.ValidateRow(c.Request.Context(), req))
}

// Quote — POST /internal/billing/quote (п.7.1 Bulk_Service_TZ).
func (h *InternalHandler) Quote(c *gin.Context) {
	var req struct {
		UserID   string `json:"userId"`
		Count    int    `json:"count"`
		Revision string `json:"revision"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError(err.Error()))
		return
	}

	result, err := h.quote.Execute(c.Request.Context(), req.UserID, req.Count, req.Revision)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// BlockBatch — POST /internal/billing/block-batch (п.7.1 Bulk_Service_TZ).
func (h *InternalHandler) BlockBatch(c *gin.Context) {
	var req struct {
		UserID  string `json:"userId"`
		Count   int    `json:"count"`
		BatchID string `json:"batchId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError(err.Error()))
		return
	}

	txIDs, err := h.bulk.BlockBatch(c.Request.Context(), req.UserID, req.Count, req.BatchID)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"transactionIds": txIDs})
}

// ListRevisions — GET /internal/revisions (capabilities уточнения ТЗ §4 п.1).
// Используется Verification Service для получения списка ревизий.
func (h *InternalHandler) ListRevisions(c *gin.Context) {
	respondRevisionList(c, h.revisionStore)
}

// GetRevisionSchema — GET /internal/revisions/:revision/schema (capabilities уточнения ТЗ §4 п.1).
// Используется Verification Service для получения детальной схемы полей конкретной ревизии
// (маппинг AAMVA-кодов при декодировании).
func (h *InternalHandler) GetRevisionSchema(c *gin.Context) {
	revision := c.Param("revision")
	schema, err := h.revisionSchema.Execute(c.Request.Context(), revision)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, schema)
}

// GenerateRaw — POST /internal/capabilities/generate-raw (capabilities уточнения ТЗ §4 п.2-5).
// Принимает «сырую» ANSI-строку от Verification Service, проксирует в BarcodeGen
// POST /internal/v1/generate/raw и возвращает imageUrl.
// Биллинг не списывается — служебная/демо генерация.
func (h *InternalHandler) GenerateRaw(c *gin.Context) {
	var req domain.GenerateRawRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError(err.Error()))
		return
	}
	resp, err := h.barcode.GenerateRaw(c.Request.Context(), req)
	if err != nil {
		RespondError(c, domain.NewBarcodeGenError(err))
		return
	}
	c.JSON(http.StatusOK, resp)
}
