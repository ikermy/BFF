package gintransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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
