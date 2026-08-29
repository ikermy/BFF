package legacy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ikermy/BFF/internal/adapters/barcodegen"
	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
)

// LegacyClient — ACL-адаптер (anti-corruption layer) для реального BarcodeGen
// (отчёт §4). Реализует порт ports.BarcodeGenClient, но говорит с BarcodeGen на его
// языке: /api/v1/barcodes/*, AAMVA-values, сервисный JWT. Используется при
// BARCODEGEN_MODE=legacy, пока BarcodeGen не приведён к контракту ТЗ.
type LegacyClient struct {
	baseURL   string // BarcodeGen /api/v1/barcodes/*  (BARCODEGEN_URL)
	rawURL    string // barcode-raw-svc для GenerateRaw   (BARCODEGEN_RAW_URL)
	client    *http.Client
	minter    *tokenMinter
	idem      ports.IdempotencyStore // идемпотентность генераций (A6); nil = выключена
	artifacts ArtifactStore          // перекладка PNG (D2 этап 2); nil = passthrough
}

// NewLegacyClient создаёт legacy-адаптер BarcodeGenClient.
// accessSecret — общий JWT_ACCESS_SECRET (fallback JWT_SECRET) для минта сервисных JWT.
// rawURL — базовый URL barcode-raw-svc (может быть пустым → GenerateRaw вернёт ошибку).
func NewLegacyClient(baseURL, accessSecret, rawURL string, timeout time.Duration) *LegacyClient {
	return &LegacyClient{
		baseURL: baseURL,
		rawURL:  rawURL,
		client:  &http.Client{Timeout: timeout},
		minter:  newTokenMinter(accessSecret),
	}
}

// WithIdempotencyStore включает идемпотентность генераций (A6, отчёт §4.2 п.6).
// Ключи: barcodegen:idem:{X-Idempotency-Key}. BarcodeGen сам не дедуплицирует —
// ретраи usecase'а (1s/3s/5s) повторяют POST, создавая дубли записей. Кэш ответа
// на уровне адаптера возвращает готовый результат вместо повторного вызова.
func (c *LegacyClient) WithIdempotencyStore(store ports.IdempotencyStore) *LegacyClient {
	c.idem = store
	return c
}

// WithArtifactStore включает перекладку сгенерированных PNG в стабильное хранилище
// (D2, этап 2). Если не вызван — используется passthrough (URL как отдал BarcodeGen).
func (c *LegacyClient) WithArtifactStore(store ArtifactStore) *LegacyClient {
	c.artifacts = store
	return c
}

// idemKeyPrefix — namespace для ключей идемпотентности генераций BarcodeGen.
const idemKeyPrefix = "barcodegen:idem:"

// idempotentExecute выполняет fn под идемпотентным ключом (A6).
//   - key пуст или store не задан → прямой вызов fn.
//   - ответ уже сохранён (Get hit) → возвращаем кэш без вызова BarcodeGen.
//   - иначе Reserve (in-flight), вызываем fn; при ошибке Delete (разрешить ретрай),
//     при успехе Set (сохранить ответ для повторных вызовов с тем же ключом).
func (c *LegacyClient) idempotentExecute(ctx context.Context, key string, fn func() ([]byte, error)) ([]byte, error) {
	if key == "" || c.idem == nil {
		return fn()
	}
	fullKey := idemKeyPrefix + key

	// 1. Уже завершённый ответ?
	if body, hit, err := c.idem.Get(ctx, fullKey); err == nil && hit {
		return body, nil
	}

	// 2. Резервируем как in-flight (первый вызов).
	reserved, rerr := c.idem.Reserve(ctx, fullKey)
	if rerr == nil && !reserved {
		// Параллельный/повторный запрос с тем же ключом — ждём завершения первого.
		for i := 0; i < 10; i++ {
			if body, hit, gerr := c.idem.Get(ctx, fullKey); gerr == nil && hit {
				return body, nil
			}
			time.Sleep(50 * time.Millisecond)
		}
		// Не дождались (редкий edge-case) — выполняем напрямую (деградация).
		return fn()
	}

	body, ferr := fn()
	if ferr != nil {
		// Снимаем in-flight маркер, чтобы ретрай мог повторно зарезервировать ключ.
		if rerr == nil && reserved {
			_ = c.idem.Delete(ctx, fullKey)
		}
		return nil, ferr
	}
	_ = c.idem.Set(ctx, fullKey, body)
	return body, nil
}

// ─── DTO BarcodeGen ───────────────────────────────────────────────────────────

// prismaBarcode — ответ BarcodeGen на генерацию (сырая Prisma-запись).
type prismaBarcode struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Type      string `json:"type"`
	Data      string `json:"data"`
	UserID    string `json:"userId"`
	EditFlag  bool   `json:"editFlag"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type pdf417Req struct {
	Values map[string]any `json:"values"`
}

type code128Req struct {
	Value string `json:"value"`
}

type fieldSetReq struct {
	Input  map[string]any `json:"input"`
	Output []string       `json:"output"`
}

// ─── GeneratePDF417 (A2–A5, A6) ──────────────────────────────────────────────

func (c *LegacyClient) GeneratePDF417(ctx context.Context, req domain.GeneratePDF417Request) (domain.GeneratePDF417Response, error) {
	raw, err := c.idempotentExecute(ctx, req.IdempotencyKey, func() ([]byte, error) {
		resp, e := c.generatePDF417(ctx, req)
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

func (c *LegacyClient) generatePDF417(ctx context.Context, req domain.GeneratePDF417Request) (domain.GeneratePDF417Response, error) {
	values := mapFields(req.Fields)
	if req.Revision != "" {
		rev, err := resolveRevision(req.Revision)
		if err != nil {
			return domain.GeneratePDF417Response{}, fmt.Errorf("barcodegen: revision: %w", err)
		}
		// Вбрасываем мост ревизии: DAJ/DBB уже в whitelist ValuesDto.
		values["DAJ"] = rev["DAJ"]
		values["DDB"] = rev["DDB"]
	}

	var bc prismaBarcode
	if err := c.post(ctx, "/api/v1/barcodes/pdf417", pdf417Req{Values: values}, &bc); err != nil {
		return domain.GeneratePDF417Response{}, err
	}
	publicURL, err := c.relocate(ctx, bc.URL, req.BuildID+":"+req.IdempotencyKey, "png")
	if err != nil {
		return domain.GeneratePDF417Response{}, err
	}
	return domain.GeneratePDF417Response{
		Success:    true,
		BarcodeURL: publicURL,
		Format:     "PDF417",
		Metadata:   domain.BarcodeMetadata{DataLength: len(bc.Data)},
	}, nil
}

// ─── GenerateCode128 (A4, A6) ────────────────────────────────────────────────

func (c *LegacyClient) GenerateCode128(ctx context.Context, req domain.GenerateCode128Request) (domain.GenerateCode128Response, error) {
	raw, err := c.idempotentExecute(ctx, req.IdempotencyKey, func() ([]byte, error) {
		resp, e := c.generateCode128(ctx, req)
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

func (c *LegacyClient) generateCode128(ctx context.Context, req domain.GenerateCode128Request) (domain.GenerateCode128Response, error) {
	if len([]rune(req.Data)) > 25 {
		return domain.GenerateCode128Response{}, fmt.Errorf("barcodegen: value must be <= 25 characters")
	}
	var bc prismaBarcode
	if err := c.post(ctx, "/api/v1/barcodes/code128", code128Req{Value: req.Data}, &bc); err != nil {
		return domain.GenerateCode128Response{}, err
	}
	publicURL, err := c.relocate(ctx, bc.URL, req.BuildID+":"+req.IdempotencyKey, "png")
	if err != nil {
		return domain.GenerateCode128Response{}, err
	}
	return domain.GenerateCode128Response{
		Success:    true,
		BarcodeURL: publicURL,
		Format:     "Code128",
		Metadata:   domain.BarcodeMetadata{EncodedData: req.Data},
	}, nil
}

// relocate перекладывает артефакт в стабильное хранилище, если оно настроено.
func (c *LegacyClient) relocate(ctx context.Context, srcURL, key, ext string) (string, error) {
	if c.artifacts == nil {
		return srcURL, nil
	}
	return c.artifacts.Save(ctx, srcURL, key, ext)
}

// ─── Calculate (B3) ──────────────────────────────────────────────────────────

// calculateOutputs — какие поля умеет calculate в BarcodeGen (отчёт §2).
var calculateOutputs = map[string][]string{
	"DBA": {"DBA"},
	"DCK": {"DCK"},
	"DCF": {"DCF"},
}

func (c *LegacyClient) Calculate(ctx context.Context, revision, field string, knownFields map[string]any) (any, error) {
	code := toAAMVA(field)
	output, ok := calculateOutputs[code]
	if !ok {
		return nil, fmt.Errorf("barcodegen: FIELD_SOURCE_UNSUPPORTED: calculate %s", field)
	}
	body := fieldSetReq{Input: mapFields(knownFields), Output: output}
	var resp map[string]any
	if err := c.post(ctx, "/api/v1/barcodes/calculate", body, &resp); err != nil {
		return nil, err
	}
	return extractField(resp, code), nil
}

// ─── Random (B3) ─────────────────────────────────────────────────────────────

// randomOutputs — соответствие поля → содержащий его random-набор (отчёт §2).
var randomOutputs = map[string][]string{
	"DAQ": {"DAQ"},
	"DBB": {"DBB"},
	"DAG": {"DAG", "DAI", "DAK"},
	"DAI": {"DAG", "DAI", "DAK"},
	"DAK": {"DAG", "DAI", "DAK"},
	"DAC": {"DAC", "DAD", "DCS"},
	"DAD": {"DAC", "DAD", "DCS"},
	"DCS": {"DAC", "DAD", "DCS"},
	"DBD": {"DBD", "DBA"},
	"DBA": {"DBD", "DBA"},
	"DCJ": {"DCJ"},
}

func (c *LegacyClient) Random(ctx context.Context, revision, field string, params map[string]any) (any, error) {
	code := toAAMVA(field)
	output, ok := randomOutputs[code]
	if !ok {
		return nil, fmt.Errorf("barcodegen: FIELD_SOURCE_UNSUPPORTED: random %s", field)
	}
	body := fieldSetReq{Input: mapFields(nil), Output: output}
	var resp map[string]any
	if err := c.post(ctx, "/api/v1/barcodes/random", body, &resp); err != nil {
		return nil, err
	}
	return extractField(resp, code), nil
}

// extractField достаёт значение запрошенного поля из ответа random/calculate,
// где ключи — человекочитаемые лейблы из field_names.json (реверс-маппинг).
func extractField(resp map[string]any, code string) any {
	if v, ok := resp[code]; ok {
		return v
	}
	for label, aamva := range labelToAAMVA {
		if aamva == code {
			if v, ok := resp[label]; ok {
				return v
			}
		}
	}
	return nil
}

// ─── GenerateRaw (raw-функция отсутствует в ядре) ────────────────────────────

func (c *LegacyClient) GenerateRaw(ctx context.Context, req domain.GenerateRawRequest) (domain.GenerateRawResponse, error) {
	if c.rawURL == "" {
		return domain.GenerateRawResponse{}, fmt.Errorf("barcodegen: BARCODEGEN_RAW_URL not configured")
	}
	type rawReqBody struct {
		NormalizedRaw string `json:"normalizedRaw"`
		Format        string `json:"format"`
	}
	type rawRespBody struct {
		ImageUrl string `json:"imageUrl"`
	}
	var resp rawRespBody
	if err := c.postRaw(ctx, "/generate/raw", rawReqBody{NormalizedRaw: req.NormalizedRaw, Format: req.Format}, &resp); err != nil {
		return domain.GenerateRawResponse{}, err
	}
	return domain.GenerateRawResponse{ImageUrl: resp.ImageUrl}, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// post выполняет POST к BarcodeGen с сервисным JWT в Authorization.
func (c *LegacyClient) post(ctx context.Context, path string, body any, out any) error {
	userID, _ := barcodegen.UserIDFromContext(ctx)
	token, err := c.minter.Mint(userID)
	if err != nil {
		return fmt.Errorf("barcodegen: mint token: %w", err)
	}
	return c.do(ctx, c.baseURL+path, body, token, out)
}

// postRaw выполняет POST к barcode-raw-svc (GenerateRaw не требует BarcodeGen-JWT).
func (c *LegacyClient) postRaw(ctx context.Context, path string, body any, out any) error {
	return c.do(ctx, c.rawURL+path, body, "", out)
}

func (c *LegacyClient) do(ctx context.Context, url string, body any, bearer string, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("barcodegen: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("barcodegen: build request %s: %w", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("barcodegen: %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("barcodegen: %s: status %d: %s", url, resp.StatusCode, string(errBody))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
