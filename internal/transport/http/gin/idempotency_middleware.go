package gintransport

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/metrics"
	"github.com/ikermy/BFF/internal/ports"

	"github.com/gin-gonic/gin"
)

// responseCapture — обёртка над gin.ResponseWriter для захвата тела ответа.
type responseCapture struct {
	gin.ResponseWriter
	body   bytes.Buffer
	status int
}

func (r *responseCapture) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func (r *responseCapture) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseCapture) Status() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

// IdempotencyMiddleware — защита write-операций от дубликатов (п.14.1, п.14.2 ТЗ).
// Если enableIdempotency=false — пропускает проверку (ENABLE_IDEMPOTENCY=false, п.15 ТЗ).
func IdempotencyMiddleware(store ports.IdempotencyStore, enableIdempotency ...bool) gin.HandlerFunc {
	enabled := true
	if len(enableIdempotency) > 0 {
		enabled = enableIdempotency[0]
	}
	return func(c *gin.Context) {
		if !enabled {
			c.Next()
			return
		}

		key := c.GetHeader("X-Idempotency-Key")
		if key == "" {
			c.Next()
			return
		}

		// Фаза 1b: проверяем готовый кэш
		cached, found, err := store.Get(c.Request.Context(), key)
		if err == nil && found {
			metrics.DuplicateRequestsTotal.Inc()
			c.Header("X-Idempotency-Replayed", "true")
			c.Data(http.StatusOK, "application/json", markDuplicateResponse(cached))
			c.Abort()
			return
		}

		// Фаза 1c: резервируем ключ (SetNX = checkOrSet(key, null) из ТЗ)
		reserved, err := store.Reserve(c.Request.Context(), key)
		if err != nil {
			c.JSON(http.StatusConflict, ErrorResponse{
				Code:    "REQUEST_IN_FLIGHT",
				Message: "a request with this idempotency key is already being processed",
			})
			c.Abort()
			return
		}
		if !reserved {
			cached, found, getErr := store.Get(c.Request.Context(), key)
			if getErr == nil && found {
				metrics.DuplicateRequestsTotal.Inc()
				c.Header("X-Idempotency-Replayed", "true")
				c.Data(http.StatusOK, "application/json", markDuplicateResponse(cached))
				c.Abort()
				return
			}
			// Параллельный запрос с тем же ключом уже in-flight
			c.JSON(http.StatusConflict, ErrorResponse{
				Code:    "REQUEST_IN_FLIGHT",
				Message: "a request with this idempotency key is already being processed",
			})
			c.Abort()
			return
		}

		// Фаза 2a: захватываем ответ хендлера
		capture := &responseCapture{ResponseWriter: c.Writer}
		c.Writer = capture
		c.Next()

		// Фаза 2b: сохраняем только успешные ответы (checkOrSet(key, response) из ТЗ)
		if capture.Status() >= 200 && capture.Status() < 300 && capture.body.Len() > 0 {
			_ = store.Set(c.Request.Context(), key, capture.body.Bytes())
		}
	}
}

func markDuplicateResponse(body []byte) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	payload["code"] = domain.ErrCodeDuplicateRequest
	encoded, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return encoded
}

// idempotencyKeyRequired — отклоняет запросы без X-Idempotency-Key (п.14.1 ТЗ).
// Применяется к write-операциям, где ключ обязателен.
func idempotencyKeyRequired(enabled bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enabled {
			c.Next()
			return
		}
		if c.GetHeader("X-Idempotency-Key") == "" {
			RespondError(c, domain.NewValidationError("X-Idempotency-Key header is required"))
			c.Abort()
			return
		}
		c.Next()
	}
}
