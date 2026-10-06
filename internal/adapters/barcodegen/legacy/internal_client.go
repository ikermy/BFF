package legacy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ikermy/BFF/internal/adapters/barcodegen"
	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
)

// InternalClient — BFF-адаптер к internal namespace BarcodeGen
// (/api/internal/v1/barcodes/*), защищённому общим BARCODEGEN_SERVICE_TOKEN.
// Используется при BARCODEGEN_MODE=internal.
//
// Отличия от LegacyClient: вместо минта пользовательского JWT шлём X-Service-Token;
// random/calculate принимают {input, outputs} и возвращают {values} (AAMVA-keyed);
// pdf417 принимает {ownerId, renderKey, values} и не дергает Billing/Kafka.
//
// Code128/Generate/GenerateRaw не входят в internal namespace — они делегируются
// встроенному LegacyClient (public контракт), чтобы не ломать существующие потоки.
type InternalClient struct {
	*LegacyClient
	serviceToken string
}

// NewInternalClient создаёт адаптер internal namespace.
// accessSecret/rawURL используются только унаследованным LegacyClient для
// fallback-операций (Code128/Generate/GenerateRaw).
func NewInternalClient(baseURL, accessSecret, serviceToken, rawURL string, timeout time.Duration) *InternalClient {
	return &InternalClient{
		LegacyClient: NewLegacyClient(baseURL, accessSecret, rawURL, timeout),
		serviceToken: serviceToken,
	}
}

// WithIdempotencyStore включает дедупликацию генераций (возвращает *InternalClient,
// чтобы сохранить internal-реализацию при чейнинге).
func (c *InternalClient) WithIdempotencyStore(store ports.IdempotencyStore) *InternalClient {
	c.idem = store
	return c
}

// WithArtifactStore включает перекладку PNG (возвращает *InternalClient).
func (c *InternalClient) WithArtifactStore(store ArtifactStore) *InternalClient {
	c.artifacts = store
	return c
}

// ipost выполняет POST к internal namespace с X-Service-Token.
func (c *InternalClient) ipost(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("barcodegen(internal): marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("barcodegen(internal): build request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Основной способ (ПЛАН §3.6) — Authorization: Bearer; X-Service-Token оставлен для совместимости.
	req.Header.Set("Authorization", "Bearer "+c.serviceToken)
	req.Header.Set("X-Service-Token", c.serviceToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("barcodegen(internal): %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("barcodegen(internal): %s: status %d: %s", path, resp.StatusCode, string(errBody))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// mapFieldsEngine переводит человекочитаемые имена в AAMVA и пропускает все
// ключи формата engine-key (3 × [A-Z0-9]), не ограничиваясь старым ValuesDto
// whitelist — internal Engine DTO принимает расширенный набор кодов.
func mapFieldsEngine(fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		code := toAAMVA(k)
		if !isEngineKey(code) {
			continue
		}
		if _, exists := out[code]; !exists {
			out[code] = v
		}
	}
	return out
}

func isEngineKey(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

type internalFieldSetReq struct {
	Input   map[string]any `json:"input"`
	Outputs []string       `json:"outputs"`
}

type internalValuesResp struct {
	Values map[string]any `json:"values"`
}

// Derive — grouped derive через internal namespace: {input, outputs} → {values}.
// revision добавляется как DAJ/DDB identity (вход шага может их не содержать).
func (c *InternalClient) Derive(ctx context.Context, revision, endpoint string, input map[string]any, output []string) (map[string]any, error) {
	if endpoint != "random" && endpoint != "calculate" {
		return nil, fmt.Errorf("barcodegen(internal): unsupported derive endpoint: %s", endpoint)
	}
	withRev := c.withRevision(mapFieldsEngine(input), revision)
	var resp internalValuesResp
	if err := c.ipost(ctx, "/api/internal/v1/barcodes/"+endpoint, internalFieldSetReq{Input: withRev, Outputs: output}, &resp); err != nil {
		return nil, err
	}
	return resp.Values, nil
}

// Random — POST /api/internal/v1/barcodes/random.
func (c *InternalClient) Random(ctx context.Context, revision, field string, params map[string]any) (any, error) {
	code := toAAMVA(field)
	output, ok := randomOutputs[code]
	if !ok {
		return nil, fmt.Errorf("barcodegen: FIELD_SOURCE_UNSUPPORTED: random %s", field)
	}
	input := c.withRevision(mapFieldsEngine(params), revision)
	var resp internalValuesResp
	if err := c.ipost(ctx, "/api/internal/v1/barcodes/random", internalFieldSetReq{Input: input, Outputs: output}, &resp); err != nil {
		return nil, err
	}
	return extractField(resp.Values, code), nil
}

// Calculate — POST /api/internal/v1/barcodes/calculate.
func (c *InternalClient) Calculate(ctx context.Context, revision, field string, knownFields map[string]any) (any, error) {
	code := toAAMVA(field)
	output, ok := calculateOutputs[code]
	if !ok {
		return nil, fmt.Errorf("barcodegen: FIELD_SOURCE_UNSUPPORTED: calculate %s", field)
	}
	input := c.withRevision(mapFieldsEngine(knownFields), revision)
	var resp internalValuesResp
	if err := c.ipost(ctx, "/api/internal/v1/barcodes/calculate", internalFieldSetReq{Input: input, Outputs: output}, &resp); err != nil {
		return nil, err
	}
	return extractField(resp.Values, code), nil
}

// RenderStatus — GET /api/internal/v1/barcodes/renders/{renderKey} (ПЛАН §B.5).
func (c *InternalClient) RenderStatus(ctx context.Context, renderKey string) (domain.RenderStatusResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/internal/v1/barcodes/renders/"+url.PathEscape(renderKey), nil)
	if err != nil {
		return domain.RenderStatusResult{}, fmt.Errorf("barcodegen(internal): build render status: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.serviceToken)
	req.Header.Set("X-Service-Token", c.serviceToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return domain.RenderStatusResult{}, fmt.Errorf("barcodegen(internal): render status: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return domain.RenderStatusResult{Status: "FAILED", ErrorCategory: "NOT_FOUND"}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return domain.RenderStatusResult{}, fmt.Errorf("barcodegen(internal): render status: status %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Status        string `json:"status"`
		URL           string `json:"url"`
		Type          string `json:"type"`
		ErrorCategory string `json:"errorCategory"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return domain.RenderStatusResult{}, fmt.Errorf("barcodegen(internal): decode render status: %w", err)
	}
	format := ""
	if strings.EqualFold(out.Type, "PDF417") {
		format = "pdf417"
	}
	return domain.RenderStatusResult{Status: out.Status, BarcodeURL: out.URL, Format: format, ErrorCategory: out.ErrorCategory}, nil
}

// withRevision добавляет DAJ/DDB из ревизии, если их ещё нет во входе.
func (c *InternalClient) withRevision(input map[string]any, revision string) map[string]any {
	return withRevisionIdentity(input, revision)
}

// withRevisionIdentity — общий для internal/legacy адаптеров bridge: BFF-имя
// ревизии US_<STATE>_<DATE> → DAJ/DDB в карте полей (если их там ещё нет).
func withRevisionIdentity(input map[string]any, revision string) map[string]any {
	if revision == "" {
		return input
	}
	rev, err := resolveRevision(revision)
	if err != nil {
		return input
	}
	for _, k := range []string{"DAJ", "DDB"} {
		if _, exists := input[k]; !exists {
			if v, ok := rev[k]; ok {
				input[k] = v
			}
		}
	}
	return input
}

type internalPDF417Req struct {
	OwnerID   string         `json:"ownerId"`
	RenderKey string         `json:"renderKey,omitempty"`
	Values    map[string]any `json:"values"`
}

type internalPDF417Resp struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Type string `json:"type,omitempty"`
}

// GeneratePDF417 — POST /api/internal/v1/barcodes/pdf417 (Billing/Kafka не вызываются).
func (c *InternalClient) GeneratePDF417(ctx context.Context, req domain.GeneratePDF417Request) (domain.GeneratePDF417Response, error) {
	raw, err := c.idempotentExecute(ctx, req.IdempotencyKey, func() ([]byte, error) {
		resp, e := c.generatePDF417Internal(ctx, req)
		if e != nil {
			return nil, e
		}
		return json.Marshal(resp)
	})
	if err != nil {
		return domain.GeneratePDF417Response{}, err
	}
	var resp domain.GeneratePDF417Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return domain.GeneratePDF417Response{}, err
	}
	return resp, nil
}

type internalCode128Req struct {
	OwnerID   string `json:"ownerId"`
	RenderKey string `json:"renderKey,omitempty"`
	Value     string `json:"value"`
}

type internalCode128Resp struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Type string `json:"type,omitempty"`
}

// GenerateCode128 — POST /api/internal/v1/barcodes/code128 (без Billing/Kafka).
func (c *InternalClient) GenerateCode128(ctx context.Context, req domain.GenerateCode128Request) (domain.GenerateCode128Response, error) {
	raw, err := c.idempotentExecute(ctx, req.IdempotencyKey, func() ([]byte, error) {
		resp, e := c.generateCode128Internal(ctx, req)
		if e != nil {
			return nil, e
		}
		return json.Marshal(resp)
	})
	if err != nil {
		return domain.GenerateCode128Response{}, err
	}
	var resp domain.GenerateCode128Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return domain.GenerateCode128Response{}, err
	}
	return resp, nil
}

func (c *InternalClient) generateCode128Internal(ctx context.Context, req domain.GenerateCode128Request) (domain.GenerateCode128Response, error) {
	if len([]rune(req.Data)) > 25 {
		return domain.GenerateCode128Response{}, fmt.Errorf("barcodegen: value must be <= 25 characters")
	}
	ownerID, _ := barcodegen.UserIDFromContext(ctx)
	renderKey := req.IdempotencyKey
	if renderKey == "" {
		renderKey = req.BuildID
	}

	var bc internalCode128Resp
	if err := c.ipost(ctx, "/api/internal/v1/barcodes/code128", internalCode128Req{
		OwnerID:   ownerID,
		RenderKey: renderKey,
		Value:     req.Data,
	}, &bc); err != nil {
		return domain.GenerateCode128Response{}, err
	}

	publicURL, err := c.relocate(ctx, bc.URL, req.BuildID+":"+req.IdempotencyKey, "png")
	if err != nil {
		return domain.GenerateCode128Response{}, err
	}
	format := bc.Type
	if format == "" {
		format = "Code128"
	}
	return domain.GenerateCode128Response{
		Success:    true,
		BarcodeURL: publicURL,
		Format:     format,
		Metadata:   domain.BarcodeMetadata{EncodedData: req.Data},
	}, nil
}

func (c *InternalClient) generatePDF417Internal(ctx context.Context, req domain.GeneratePDF417Request) (domain.GeneratePDF417Response, error) {
	values := c.withRevision(mapFieldsEngine(req.Fields), req.Revision)

	ownerID, _ := barcodegen.UserIDFromContext(ctx)
	renderKey := req.IdempotencyKey
	if renderKey == "" {
		renderKey = req.BuildID
	}

	var bc internalPDF417Resp
	if err := c.ipost(ctx, "/api/internal/v1/barcodes/pdf417", internalPDF417Req{
		OwnerID:   ownerID,
		RenderKey: renderKey,
		Values:    values,
	}, &bc); err != nil {
		return domain.GeneratePDF417Response{}, err
	}

	publicURL, err := c.relocate(ctx, bc.URL, req.BuildID+":"+req.IdempotencyKey, "png")
	if err != nil {
		return domain.GeneratePDF417Response{}, err
	}
	format := bc.Type
	if format == "" {
		format = "PDF417"
	}
	return domain.GeneratePDF417Response{
		Success:    true,
		BarcodeURL: publicURL,
		Format:     format,
		Metadata:   domain.BarcodeMetadata{DataLength: len(bc.ID)},
	}, nil
}
