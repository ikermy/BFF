package gintransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ikermy/BFF/internal/adapters/idempotency"
)

type sequenceIdempotencyStore struct {
	getCalls int
	body     []byte
}

func (s *sequenceIdempotencyStore) Get(_ context.Context, _ string) ([]byte, bool, error) {
	s.getCalls++
	if s.getCalls == 1 {
		return nil, false, nil
	}
	return s.body, true, nil
}

func (s *sequenceIdempotencyStore) Reserve(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (s *sequenceIdempotencyStore) Set(_ context.Context, _ string, _ []byte) error {
	return nil
}

func (s *sequenceIdempotencyStore) Delete(_ context.Context, _ string) error {
	return nil
}

func TestIdempotencyMiddleware_ReturnsDuplicateRequestWhenCacheAppearsAfterReserveRace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &sequenceIdempotencyStore{body: []byte(`{"success":true,"buildId":"b1"}`)}

	r := gin.New()
	r.POST("/test", IdempotencyMiddleware(store), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-Idempotency-Key", "dup-1")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-Idempotency-Replayed") != "true" {
		t.Fatalf("expected X-Idempotency-Replayed=true, got %q", w.Header().Get("X-Idempotency-Replayed"))
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["code"] != "DUPLICATE_REQUEST" {
		t.Fatalf("expected code=DUPLICATE_REQUEST, got %v", resp)
	}
	if resp["success"] != true || resp["buildId"] != "b1" {
		t.Fatalf("expected cached body to be preserved, got %v", resp)
	}
}

// TestIdempotencyMiddleware_DeletesMarkerOnHandlerError проверяет, что после ошибки
// хендлера (не-2xx) in-flight маркер удаляется и клиент может ретраить немедленно.
// Воспроизводит баг: без Delete ретрай получает 409 REQUEST_IN_FLIGHT до истечения TTL.
func TestIdempotencyMiddleware_DeletesMarkerOnHandlerError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := idempotency.NewMemoryStore(10 * time.Second)

	r := gin.New()
	r.POST("/test", IdempotencyMiddleware(store), func(c *gin.Context) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "downstream failed"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-Idempotency-Key", "err-key-1")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}

	// Ключ должен быть свободен: повторный Reserve должен успешно зарезервировать.
	// До фикса: Reserve вернул бы false (маркер висит), клиент заблокирован до TTL.
	ok, err := store.Reserve(context.Background(), "err-key-1")
	if err != nil {
		t.Fatalf("Reserve after error: unexpected err: %v", err)
	}
	if !ok {
		t.Error("in-flight marker must be deleted after 503: client must be able to retry")
	}
}

// TestIdempotencyMiddleware_KeepsMarkerOn2xx проверяет, что при успехе хендлера
// маркер НЕ удаляется, а заменяется готовым ответом через Set.
func TestIdempotencyMiddleware_KeepsMarkerOn2xx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := idempotency.NewMemoryStore(10 * time.Second)

	r := gin.New()
	r.POST("/test", IdempotencyMiddleware(store), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-Idempotency-Key", "ok-key-1")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Ответ должен быть закэширован: Get должен вернуть found=true
	body, found, err := store.Get(context.Background(), "ok-key-1")
	if err != nil || !found {
		t.Fatalf("expected cached response after 200, found=%v err=%v", found, err)
	}
	if len(body) == 0 {
		t.Error("expected non-empty cached body")
	}
}
