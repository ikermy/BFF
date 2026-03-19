package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
)

// defaultTimeout — дефолтный таймаут для Auth Service.
// ТЗ не задаёт явное значение; 5s достаточно для валидации токена.
const defaultTimeout = 5 * time.Second

// HTTPClient — production-реализация AuthClient (п.11.1 ТЗ).
// Вызывает Legacy Auth Service через HTTP.
// ValidateToken: POST /api/v1/validate
// GetUserInfo:   GET  /api/v1/users/{userId}
type HTTPClient struct {
	baseURL      string
	httpClient   *http.Client
	timeoutStore ports.TimeoutStore // динамический таймаут через PUT /admin/config/timeouts
}

// NewHTTPClient создаёт HTTP-клиент для Legacy Auth Service.
// baseURL берётся из cfg.Services.AuthURL (AUTH_URL, п.17.1 ТЗ).
func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

// WithTimeouts подключает TimeoutStore для динамического таймаута.
// Таймаут берётся из ServiceTimeouts.Auth; 0 = использовать defaultTimeout.
func (c *HTTPClient) WithTimeouts(store ports.TimeoutStore) *HTTPClient {
	c.timeoutStore = store
	return c
}

// ctxWithTimeout применяет таймаут из store или defaultTimeout.
func (c *HTTPClient) ctxWithTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeoutStore != nil {
		if t, err := c.timeoutStore.Get(ctx); err == nil && t.Auth > 0 {
			return context.WithTimeout(ctx, time.Duration(t.Auth)*time.Millisecond)
		}
	}
	return context.WithTimeout(ctx, defaultTimeout)
}

// ─── DTO запросов/ответов ─────────────────────────────────────────────────────

type validateReqBody struct {
	Token string `json:"token"`
}

// validateRespBody — ответ POST /api/v1/validate.
// TypeScript: { userId, email, permissions } — role отсутствует в validate-ответе.
type validateRespBody struct {
	UserID      string   `json:"userId"`
	Email       string   `json:"email"`
	Permissions []string `json:"permissions,omitempty"`
}

// ─── AuthClient interface ─────────────────────────────────────────────────────

// ValidateToken — POST /api/v1/validate (п.11.1, п.16.1 ТЗ).
// TypeScript: POST {authUrl}/api/v1/validate { token } → { userId, email, permissions }.
func (c *HTTPClient) ValidateToken(ctx context.Context, token string) (domain.UserInfo, error) {
	body := validateReqBody{Token: token}

	var resp validateRespBody
	if err := c.post(ctx, "/api/v1/validate", body, &resp); err != nil {
		return domain.UserInfo{}, err
	}
	return domain.UserInfo{
		UserID:      resp.UserID,
		Email:       resp.Email,
		Permissions: resp.Permissions,
	}, nil
}

// GetUserInfo — GET /api/v1/users/{userId} (п.11.1 ТЗ).
// TypeScript: GET {authUrl}/api/v1/users/{userId} → response.data (полный UserInfo).
func (c *HTTPClient) GetUserInfo(ctx context.Context, userID string) (domain.UserInfo, error) {
	if userID == "" {
		return domain.UserInfo{}, fmt.Errorf("auth: userID is required")
	}

	var resp domain.UserInfo
	if err := c.get(ctx, "/api/v1/users/"+userID, &resp); err != nil {
		return domain.UserInfo{}, err
	}
	return resp, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func (c *HTTPClient) post(ctx context.Context, path string, body any, out any) error {
	ctx, cancel := c.ctxWithTimeout(ctx)
	defer cancel()

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("auth: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("auth: build request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")

	return c.do(req, out)
}

func (c *HTTPClient) get(ctx context.Context, path string, out any) error {
	ctx, cancel := c.ctxWithTimeout(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("auth: build request %s: %w", path, err)
	}

	return c.do(req, out)
}

func (c *HTTPClient) do(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("auth: %s %s: %w", req.Method, req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("auth: %s %s: status %d: %s", req.Method, req.URL.Path, resp.StatusCode, string(errBody))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
