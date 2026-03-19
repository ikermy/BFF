package history

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
)

// defaultTimeout — дефолтный таймаут для History Service.
// ТЗ не задаёт явное значение; 5s достаточно для простого GET-запроса.
const defaultTimeout = 5 * time.Second

// HTTPClient — production-реализация HistoryClient (п.10 ТЗ).
// CheckFreeEdit: GET /internal/barcode/:id/check-free-edit
// GetBarcode:    GET /internal/barcode/:id
type HTTPClient struct {
	baseURL      string
	httpClient   *http.Client
	timeoutStore ports.TimeoutStore // динамический таймаут через PUT /admin/config/timeouts
}

// NewHTTPClient создаёт HTTP-клиент для History Service.
// baseURL берётся из cfg.Services.HistoryURL (HISTORY_URL, п.17.1 ТЗ).
func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

// WithTimeouts подключает TimeoutStore для динамического таймаута.
// Таймаут берётся из ServiceTimeouts.History; 0 = использовать defaultTimeout.
func (c *HTTPClient) WithTimeouts(store ports.TimeoutStore) *HTTPClient {
	c.timeoutStore = store
	return c
}

// ctxWithTimeout применяет таймаут из store или defaultTimeout.
func (c *HTTPClient) ctxWithTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeoutStore != nil {
		if t, err := c.timeoutStore.Get(ctx); err == nil && t.History > 0 {
			return context.WithTimeout(ctx, time.Duration(t.History)*time.Millisecond)
		}
	}
	return context.WithTimeout(ctx, defaultTimeout)
}

// ─── DTO ответов ──────────────────────────────────────────────────────────────

// checkFreeEditResp — ответ GET /internal/barcode/:id/check-free-edit (п.10.1 ТЗ).
// ТЗ: { canEdit: boolean, editFlag: boolean }
type checkFreeEditResp struct {
	CanEdit  bool `json:"canEdit"`
	EditFlag bool `json:"editFlag"`
}

// ─── HistoryClient interface ──────────────────────────────────────────────────

// CheckFreeEdit — GET /internal/barcode/:id/check-free-edit (п.10.1 ТЗ).
// editFlag=false → первое редактирование ещё не использовано → canEdit=true.
// editFlag=true  → редактирование уже использовано → canEdit=false.
func (c *HTTPClient) CheckFreeEdit(ctx context.Context, barcodeID string) (bool, error) {
	if barcodeID == "" {
		return false, fmt.Errorf("history: barcodeID is required")
	}

	var resp checkFreeEditResp
	if err := c.get(ctx, "/internal/barcode/"+barcodeID+"/check-free-edit", &resp); err != nil {
		return false, err
	}
	return resp.CanEdit, nil
}

// GetBarcode — GET /internal/barcode/:id (п.10.2 ТЗ).
// Используется для Remake: быстрое заполнение формы данными существующего баркода.
func (c *HTTPClient) GetBarcode(ctx context.Context, barcodeID string) (domain.BarcodeRecord, error) {
	if barcodeID == "" {
		return domain.BarcodeRecord{}, fmt.Errorf("history: barcodeID is required")
	}

	var resp domain.BarcodeRecord
	if err := c.get(ctx, "/internal/barcode/"+barcodeID, &resp); err != nil {
		return domain.BarcodeRecord{}, err
	}
	return resp, nil
}

// ─── helper ───────────────────────────────────────────────────────────────────

func (c *HTTPClient) get(ctx context.Context, path string, out any) error {
	ctx, cancel := c.ctxWithTimeout(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("history: build request %s: %w", path, err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("history: %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("history: %s: status %d: %s", path, resp.StatusCode, string(errBody))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
