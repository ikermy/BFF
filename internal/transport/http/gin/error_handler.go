package gintransport

import (
	"errors"
	"net/http"

	"github.com/ikermy/BFF/internal/domain"

	"github.com/gin-gonic/gin"
)

// ErrorResponse — тело JSON-ответа при ошибке.
// Details содержит дополнительные поля (например, topUpRequired для 402, п.6.3 ТЗ).
type ErrorResponse struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// RespondError — централизованный маппинг AppError → HTTP ответ (п.15.1 ТЗ).
// Если err — *domain.AppError, берём HTTPStatus, Code и Details из него.
// Иначе — 500 INTERNAL_ERROR.
func RespondError(c *gin.Context, err error) {
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		c.JSON(appErr.HTTPStatus, ErrorResponse{
			Code:    appErr.Code,
			Message: appErr.Message,
			Details: appErr.Details,
		})
		return
	}
	c.JSON(http.StatusInternalServerError, ErrorResponse{
		Code:    "INTERNAL_ERROR",
		Message: err.Error(),
	})
}
