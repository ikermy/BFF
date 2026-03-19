package billing

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

// HTTPClient — production-реализация BillingClient (п.7 ТЗ).
type HTTPClient struct {
	baseURL      string
	httpClient   *http.Client
	timeoutStore ports.TimeoutStore // динамический таймаут (п.13.2 ТЗ)
}

func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

// WithTimeouts подключает TimeoutStore для динамического таймаута (п.13.2 ТЗ).
func (c *HTTPClient) WithTimeouts(store ports.TimeoutStore) *HTTPClient {
	c.timeoutStore = store
	return c
}

func (c *HTTPClient) applyTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeoutStore == nil {
		return ctx, func() {}
	}
	t, err := c.timeoutStore.Get(ctx)
	if err != nil || t.Billing <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(t.Billing)*time.Millisecond)
}

// ─── DTO запросов/ответов ─────────────────────────────────────────────────────

type quoteReqBody struct {
	UserID  string       `json:"userId"`
	Units   int          `json:"units"`
	Context quoteContext `json:"context"`
}

// quoteContext — контекст продукта (п.7.3 ТЗ).
// TypeScript: context: { product, revision }.
type quoteContext struct {
	Product  string `json:"product"`
	Revision string `json:"revision"`
}

// quoteRespBody — ответ POST /internal/billing/quote.
// TypeScript: { allowedTotal, unitPrice, bySource: { subscription, credits, wallet } }.
// partial и shortfall могут быть как возвращены Billing, так и вычислены BFF.
type quoteRespBody struct {
	AllowedTotal int                   `json:"allowedTotal"`
	UnitPrice    float64               `json:"unitPrice"`
	BySource     domain.QuoteBreakdown `json:"bySource"`
	Partial      bool                  `json:"partial"`
	Shortfall    *domain.Shortfall     `json:"shortfall,omitempty"`
}

type blockReqBody struct {
	UserID   string                `json:"userId"`
	Units    int                   `json:"units"`
	BySource domain.QuoteBreakdown `json:"bySource"` // Split Payment разбивка (п.7.1 ТЗ)
	SagaID   string                `json:"sagaId"`
	BuildID  string                `json:"buildId,omitempty"`
	BatchID  string                `json:"batchId,omitempty"`
}

type captureReqBody struct {
	SagaID string `json:"sagaId"`
	Units  int    `json:"units"`
}

type releaseReqBody struct {
	SagaID string `json:"sagaId"`
	Units  int    `json:"units"`
}

type blockBatchReqBody struct {
	UserID  string `json:"userId"`
	Count   int    `json:"count"`
	BatchID string `json:"batchId"`
}

type blockBatchRespBody struct {
	TransactionIDs []string `json:"transactionIds"`
}

// ─── BillingClient interface ──────────────────────────────────────────────────

// Quote — POST /internal/billing/quote (п.7.3 ТЗ).
// TypeScript: { userId, units, context: { product, revision } } → { allowedTotal, unitPrice, bySource }.
// CanProcess и Partial вычисляются на стороне BFF из allowedTotal.
func (c *HTTPClient) Quote(ctx context.Context, userID string, units int, revision string) (domain.QuoteResult, error) {
	body := quoteReqBody{
		UserID: userID,
		Units:  units,
		Context: quoteContext{
			Product:  "barcode",
			Revision: revision,
		},
	}
	var resp quoteRespBody
	if err := c.post(ctx, "/internal/billing/quote", body, &resp); err != nil {
		return domain.QuoteResult{}, err
	}
	// CanProcess и Partial — логика оркестрации BFF (п.1.2 ТЗ), не Billing.
	partial := resp.Partial || resp.AllowedTotal < units
	result := domain.QuoteResult{
		CanProcess:   resp.AllowedTotal > 0,
		Partial:      partial,
		Requested:    units,
		AllowedTotal: resp.AllowedTotal,
		UnitPrice:    resp.UnitPrice,
		BySource:     resp.BySource,
		Shortfall:    resp.Shortfall,
	}
	// Если Billing не вернул shortfall — вычисляем сами (п.7.3 ТЗ).
	if partial && result.Shortfall == nil {
		result.Shortfall = &domain.Shortfall{
			Units:          units - resp.AllowedTotal,
			AmountRequired: float64(units-resp.AllowedTotal) * resp.UnitPrice,
		}
	}
	return result, nil
}

// Block — POST /internal/billing/block (п.7.3 ТЗ).
// TypeScript: { userId, units, bySource, sagaId, buildId, batchId }.
// units=0 допускается для бесплатного редактирования (п.10.1 ТЗ).
func (c *HTTPClient) Block(ctx context.Context, req domain.BlockRequest) error {
	body := blockReqBody{
		UserID:   req.UserID,
		Units:    req.Units,
		BySource: req.BySource,
		SagaID:   req.SagaID,
		BuildID:  req.BuildID,
		BatchID:  req.BatchID,
	}
	return c.post(ctx, "/internal/billing/block", body, nil)
}

// Capture — POST /internal/billing/capture (п.14.4 ТЗ).
// Списывает точное количество успешно сгенерированных единиц.
func (c *HTTPClient) Capture(ctx context.Context, sagaID string, units int) error {
	return c.post(ctx, "/internal/billing/capture", captureReqBody{SagaID: sagaID, Units: units}, nil)
}

// Release — POST /internal/billing/release (п.14.4 ТЗ).
// Разблокирует неиспользованные единицы (компенсирующая транзакция).
func (c *HTTPClient) Release(ctx context.Context, sagaID string, units int) error {
	return c.post(ctx, "/internal/billing/release", releaseReqBody{SagaID: sagaID, Units: units}, nil)
}

// BlockBatch — POST /internal/billing/block-batch (Bulk_Service_TZ п.4).
// Возвращает список transactionIDs для каждой единицы батча.
func (c *HTTPClient) BlockBatch(ctx context.Context, userID string, count int, batchID string) ([]string, error) {
	body := blockBatchReqBody{UserID: userID, Count: count, BatchID: batchID}
	var resp blockBatchRespBody
	if err := c.post(ctx, "/internal/billing/block-batch", body, &resp); err != nil {
		return nil, err
	}
	return resp.TransactionIDs, nil
}

// ─── helper ───────────────────────────────────────────────────────────────────

func (c *HTTPClient) post(ctx context.Context, path string, body any, out any) error {
	ctx, cancel := c.applyTimeout(ctx) // п.13.2: динамический таймаут из TimeoutStore
	defer cancel()
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("billing: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("billing: build request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("billing: %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("billing: %s: status %d: %s", path, resp.StatusCode, string(errBody))
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
