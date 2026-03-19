package gintransport

import (
	"net/http"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/usecase"

	"github.com/gin-gonic/gin"
)

type InternalHandler struct {
	quote *usecase.QuoteUseCase
	bulk  *usecase.BulkUseCase
}

func NewInternalHandler(quote *usecase.QuoteUseCase, bulk *usecase.BulkUseCase) *InternalHandler {
	return &InternalHandler{quote: quote, bulk: bulk}
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
