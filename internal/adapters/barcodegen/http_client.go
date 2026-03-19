package barcodegen

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

// HTTPClient — production-реализация BarcodeGenClient (п.8 ТЗ).
// Вызывает реальный BarcodeGen Service по HTTP.
// В тестах используется MockClient; HTTPClient создаётся в BuildAPIApp при наличии BARCODEGEN_URL.
type HTTPClient struct {
	baseURL      string
	httpClient   *http.Client
	timeoutStore ports.TimeoutStore // если задан — таймаут читается динамически (п.13.2 ТЗ)
}

// NewHTTPClient создаёт HTTP-клиент для BarcodeGen Service.
// timeout берётся из cfg.Timeouts.BarcodeGen (BARCODEGEN_TIMEOUT, п.17.1 ТЗ).
func NewHTTPClient(baseURL string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// WithTimeouts подключает TimeoutStore для динамического управления таймаутом (п.13.2 ТЗ).
// При каждом вызове будет читаться актуальное значение из хранилища.
func (c *HTTPClient) WithTimeouts(store ports.TimeoutStore) *HTTPClient {
	c.timeoutStore = store
	return c
}

// applyTimeout добавляет context.WithTimeout из TimeoutStore если он задан.
func (c *HTTPClient) applyTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeoutStore == nil {
		return ctx, func() {}
	}
	t, err := c.timeoutStore.Get(ctx)
	if err != nil || t.BarcodeGen <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(t.BarcodeGen)*time.Millisecond)
}

// ─── внутренние DTO запросов/ответов ─────────────────────────────────────────

type generateReqBody struct {
	BarcodeType string         `json:"barcodeType"`
	Fields      map[string]any `json:"fields"`
}

type generateRespBody struct {
	URL    string `json:"url"`
	Format string `json:"format"`
}

type calculateReqBody struct {
	Revision    string         `json:"revision"`
	Field       string         `json:"field"`
	KnownFields map[string]any `json:"knownFields"`
}

// randomReqBody — тело POST /api/v1/random (п.8.2 ТЗ).
// Соответствует TypeScript RandomRequest: { revision, field, params }.
type randomReqBody struct {
	Revision string         `json:"revision"`
	Field    string         `json:"field"`
	Params   map[string]any `json:"params,omitempty"`
}

// valueRespBody — ответ BarcodeGen для calculate и random (п.8.2 ТЗ).
// TypeScript: response.data.value
type valueRespBody struct {
	Value any `json:"value"`
}

type pdf417ReqBody struct {
	Revision string                `json:"revision"`
	BuildID  string                `json:"buildId,omitempty"`
	BatchID  string                `json:"batchId,omitempty"`
	Fields   map[string]any        `json:"fields"`
	Options  domain.BarcodeOptions `json:"options,omitempty"`
}

type code128ReqBody struct {
	Revision string                `json:"revision"`
	Fields   map[string]any        `json:"fields"` // {"data": req.Data}
	Options  domain.BarcodeOptions `json:"options,omitempty"`
}

// ─── BarcodeGenClient interface ───────────────────────────────────────────────

// Generate — POST /api/v1/generate (п.8.1 ТЗ).
// Генерирует баркод заданного типа с переданными (уже вычисленными) полями.
func (c *HTTPClient) Generate(ctx context.Context, barcodeType string, fields map[string]any) (domain.BarcodeItem, error) {
	body := generateReqBody{BarcodeType: barcodeType, Fields: fields}
	var resp generateRespBody
	if err := c.post(ctx, "/api/v1/generate", body, nil, &resp); err != nil {
		return domain.BarcodeItem{}, err
	}
	return domain.BarcodeItem{URL: resp.URL, Format: resp.Format}, nil
}

// Calculate — POST /api/v1/calculate (п.8.2 ТЗ, source=calculate).
// Рассчитывает значение поля на основе уже известных полей.
// TypeScript: POST /api/v1/calculate {revision, field, knownFields} → response.data.value
func (c *HTTPClient) Calculate(ctx context.Context, revision, field string, knownFields map[string]any) (any, error) {
	body := calculateReqBody{Revision: revision, Field: field, KnownFields: knownFields}
	var resp valueRespBody
	if err := c.post(ctx, "/api/v1/calculate", body, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// Random — POST /api/v1/random (п.8.2 ТЗ, source=random).
// Генерирует случайное значение поля с заданными параметрами.
// TypeScript: POST /api/v1/random {revision, field, params} → response.data.value
func (c *HTTPClient) Random(ctx context.Context, revision, field string, params map[string]any) (any, error) {
	body := randomReqBody{Revision: revision, Field: field, Params: params}
	var resp valueRespBody
	if err := c.post(ctx, "/api/v1/random", body, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// GeneratePDF417 — POST /api/v1/generate/pdf417 (п.12.3 ТЗ).
// IdempotencyKey форвардируется как заголовок X-Idempotency-Key (п.8.2 ТЗ).
func (c *HTTPClient) GeneratePDF417(ctx context.Context, req domain.GeneratePDF417Request) (domain.GeneratePDF417Response, error) {
	body := pdf417ReqBody{
		Revision: req.Revision,
		BuildID:  req.BuildID,
		BatchID:  req.BatchID,
		Fields:   req.Fields,
		Options:  req.Options,
	}
	headers := make(map[string]string)
	if req.IdempotencyKey != "" {
		headers["X-Idempotency-Key"] = req.IdempotencyKey
	}
	var resp domain.GeneratePDF417Response
	if err := c.post(ctx, "/api/v1/generate/pdf417", body, headers, &resp); err != nil {
		return domain.GeneratePDF417Response{}, err
	}
	return resp, nil
}

// GenerateCode128 — POST /api/v1/generate/code128 (п.12.4 ТЗ).
// IdempotencyKey форвардируется как заголовок X-Idempotency-Key (п.8.2 ТЗ).
func (c *HTTPClient) GenerateCode128(ctx context.Context, req domain.GenerateCode128Request) (domain.GenerateCode128Response, error) {
	body := code128ReqBody{
		Revision: "",
		Fields:   map[string]any{"data": req.Data}, // п.12.4: data передаётся как поле
		Options:  req.Options,
	}
	headers := make(map[string]string)
	if req.IdempotencyKey != "" {
		headers["X-Idempotency-Key"] = req.IdempotencyKey
	}
	var resp domain.GenerateCode128Response
	if err := c.post(ctx, "/api/v1/generate/code128", body, headers, &resp); err != nil {
		return domain.GenerateCode128Response{}, err
	}
	return resp, nil
}

// ─── helper ───────────────────────────────────────────────────────────────────

// post выполняет HTTP POST к BarcodeGen Service и декодирует ответ в out.
// Возвращает ошибку при не-2xx статусе.
func (c *HTTPClient) post(ctx context.Context, path string, body any, headers map[string]string, out any) error {
	ctx, cancel := c.applyTimeout(ctx) // п.13.2: динамический таймаут из TimeoutStore
	defer cancel()
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("barcodegen: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("barcodegen: build request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("barcodegen: %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("barcodegen: %s: status %d: %s", path, resp.StatusCode, string(errBody))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
