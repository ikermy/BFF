package gintransport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/metrics"
	"github.com/ikermy/BFF/internal/ports"

	"github.com/gin-gonic/gin"
)

// idemHashSuffix — суффикс ключа для сохранения hash тела запроса (ПЛАН §3.3).
const idemHashSuffix = ":hash"

// requestBodyHash возвращает sha256 тела запроса, восстанавливая Body для хендлера.
func requestBodyHash(c *gin.Context) string {
	if c.Request == nil || c.Request.Body == nil {
		return ""
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// hashMismatch: true, если для ключа сохранён hash и он не совпадает с текущим
// (повторное использование ключа с другим телом). Если hash не сохранён — false.
func hashMismatch(store ports.IdempotencyStore, c *gin.Context, key, current string) bool {
	if current == "" {
		return false
	}
	stored, found, err := store.Get(c.Request.Context(), key+idemHashSuffix)
	if err != nil || !found {
		return false
	}
	// Защита от ложных срабатываний: считаем hash только валидной 64-символьной hex-строкой.
	if len(stored) != 64 {
		return false
	}
	return string(stored) != current
}

// maxIdempotencyBodySize — максимальный размер тела ответа, сохраняемого в IdempotencyStore.
// Защита от OOM: при больших ответах (файлы, крупные batch-результаты) bytes.Buffer
// не накапливается безгранично. При превышении лимита ответ не кэшируется,
// in-flight маркер удаляется — клиент может повторить запрос немедленно.
// OOM fix: BFF_Final_Status_Report.md Техдолг п.5.
const maxIdempotencyBodySize = 1 << 20 // 1 МБ

// responseCapture — обёртка над gin.ResponseWriter для захвата тела ответа.
type responseCapture struct {
	gin.ResponseWriter
	body       bytes.Buffer
	status     int
	overflowed bool // true если ответ превысил maxIdempotencyBodySize — не кэшируем
}

func (r *responseCapture) Write(b []byte) (int, error) {
	if !r.overflowed {
		if r.body.Len()+len(b) > maxIdempotencyBodySize {
			// Превышен лимит — помечаем как overflowed и не кэшируем,
			// но ответ клиенту продолжаем отдавать в штатном режиме.
			r.overflowed = true
		} else {
			r.body.Write(b)
		}
	}
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

		reqHash := requestBodyHash(c)

		// Фаза 1b: проверяем готовый кэш
		cached, found, err := store.Get(c.Request.Context(), key)
		if err == nil && found {
			if hashMismatch(store, c, key, reqHash) {
				c.JSON(http.StatusConflict, ErrorResponse{
					Code:    "IDEMPOTENCY_KEY_REUSED",
					Message: "this idempotency key was used with a different request body",
				})
				c.Abort()
				return
			}
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
				if hashMismatch(store, c, key, reqHash) {
					c.JSON(http.StatusConflict, ErrorResponse{
						Code:    "IDEMPOTENCY_KEY_REUSED",
						Message: "this idempotency key was used with a different request body",
					})
					c.Abort()
					return
				}
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

		// Фаза 2b: сохраняем только успешные ответы (checkOrSet(key, response) из ТЗ).
		// При ошибке (не-2xx), пустом теле или превышении maxIdempotencyBodySize —
		// удаляем in-flight маркер: клиент должен иметь возможность ретраить с тем же
		// ключом немедленно, а не ждать истечения TTL, постоянно получая 409 REQUEST_IN_FLIGHT.
		if capture.Status() >= 200 && capture.Status() < 300 && capture.body.Len() > 0 && !capture.overflowed {
			_ = store.Set(c.Request.Context(), key, capture.body.Bytes())
			if reqHash != "" {
				_ = store.Set(c.Request.Context(), key+idemHashSuffix, []byte(reqHash))
			}
		} else {
			_ = store.Delete(c.Request.Context(), key)
			_ = store.Delete(c.Request.Context(), key+idemHashSuffix)
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
