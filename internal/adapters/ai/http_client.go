package ai

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

// Таймауты по п.9.2 ТЗ (соответствуют TypeScript: signature=30000, photo=60000).
const (
	signatureTimeout = 30 * time.Second
	photoTimeout     = 60 * time.Second
)

// HTTPClient — production-реализация AIClient (п.9 ТЗ).
type HTTPClient struct {
	baseURL      string
	serviceToken string
	httpClient   *http.Client
	timeoutStore ports.TimeoutStore // если задан — photo timeout берётся из store (п.13.2 ТЗ)
}

func NewHTTPClient(baseURL, serviceToken string) *HTTPClient {
	return &HTTPClient{
		baseURL:      baseURL,
		serviceToken: serviceToken,
		httpClient:   &http.Client{},
	}
}

// WithTimeouts подключает TimeoutStore (п.13.2 ТЗ).
// Применяется к GeneratePhoto; GenerateSignature всегда 30s (п.9.2 ТЗ).
func (c *HTTPClient) WithTimeouts(store ports.TimeoutStore) *HTTPClient {
	c.timeoutStore = store
	return c
}

// photoCtxTimeout возвращает таймаут для photo: из store или 60s по умолчанию.
func (c *HTTPClient) photoCtxTimeout(ctx context.Context) time.Duration {
	if c.timeoutStore != nil {
		if t, err := c.timeoutStore.Get(ctx); err == nil && t.AI > 0 {
			return time.Duration(t.AI) * time.Millisecond
		}
	}
	return photoTimeout
}

// ─── DTO запросов/ответов (внутренние, не экспортируются) ─────────────────────

type signatureReqBody struct {
	UserID string              `json:"userId"`
	SagaID string              `json:"sagaId"`
	Params signatureParamsBody `json:"params"`
}

type signatureParamsBody struct {
	Name  string `json:"name"`
	Style string `json:"style"`
}

type signatureRespBody struct {
	ImageURL string `json:"imageUrl"`
	Style    string `json:"style"`
}

type photoReqBody struct {
	UserID string          `json:"userId"`
	SagaID string          `json:"sagaId"`
	Params photoParamsBody `json:"params"`
}

type photoParamsBody struct {
	Description string `json:"description"`
	Gender      string `json:"gender,omitempty"`
	Age         int    `json:"age,omitempty"`
}

type photoRespBody struct {
	ImageURL string `json:"imageUrl"`
}

// ─── AIClient interface ───────────────────────────────────────────────────────

// GenerateSignature — POST /internal/ai/signature (п.9.2 ТЗ).
// Authorization: Bearer {serviceToken}. Таймаут: 30s.
// TypeScript: POST {aiUrl}/internal/ai/signature + Bearer + timeout 30000.
func (c *HTTPClient) GenerateSignature(ctx context.Context, req domain.AISignatureRequest) (domain.AISignatureResponse, error) {
	style := req.Style
	if style == "" {
		style = "formal"
	}
	body := signatureReqBody{
		UserID: req.UserID,
		SagaID: req.SagaID,
		Params: signatureParamsBody{Name: req.FullName, Style: style},
	}
	headers := map[string]string{
		"Authorization": "Bearer " + c.serviceToken,
	}

	ctx, cancel := context.WithTimeout(ctx, signatureTimeout)
	defer cancel()

	var resp signatureRespBody
	if err := c.post(ctx, "/internal/ai/signature", body, headers, &resp); err != nil {
		return domain.AISignatureResponse{}, err
	}
	return domain.AISignatureResponse{ImageURL: resp.ImageURL, Style: resp.Style}, nil
}

// GeneratePhoto — POST /internal/ai/photo (п.9.2 ТЗ).
// Без Authorization header (по TypeScript-реализации).
// Таймаут: из TimeoutStore.AI если подключён, иначе 60s (п.13.2 ТЗ).
func (c *HTTPClient) GeneratePhoto(ctx context.Context, req domain.AIPhotoRequest) (domain.AIPhotoResponse, error) {
	body := photoReqBody{
		UserID: req.UserID,
		SagaID: req.SagaID,
		Params: photoParamsBody{
			Description: req.Description,
			Gender:      req.Gender,
			Age:         req.Age,
		},
	}

	ctx, cancel := context.WithTimeout(ctx, c.photoCtxTimeout(ctx))
	defer cancel()

	var resp photoRespBody
	if err := c.post(ctx, "/internal/ai/photo", body, nil, &resp); err != nil {
		return domain.AIPhotoResponse{}, err
	}
	return domain.AIPhotoResponse{ImageURL: resp.ImageURL}, nil
}

// ─── helper ───────────────────────────────────────────────────────────────────

func (c *HTTPClient) post(ctx context.Context, path string, body any, headers map[string]string, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("ai: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("ai: build request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ai: %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ai: %s: status %d: %s", path, resp.StatusCode, string(errBody))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
