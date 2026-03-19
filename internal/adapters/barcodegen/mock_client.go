package barcodegen

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"

	"github.com/ikermy/BFF/internal/domain"
)

type MockClient struct {
	counter uint64
}

func NewMockClient() *MockClient {
	return &MockClient{}
}

// Generate — legacy-метод, возвращает BarcodeItem по типу баркода (п.8.1 ТЗ).
// Используется в тестах напрямую; в production-коде вызываются GeneratePDF417/GenerateCode128.
func (c *MockClient) Generate(ctx context.Context, barcodeType string, fields map[string]any) (domain.BarcodeItem, error) {
	switch barcodeType {
	case "code128":
		data, _ := fields["data"].(string)
		resp, err := c.GenerateCode128(ctx, domain.GenerateCode128Request{Data: data})
		if err != nil {
			return domain.BarcodeItem{}, err
		}
		return domain.BarcodeItem{URL: resp.BarcodeURL, Format: resp.Format}, nil
	default: // pdf417
		resp, err := c.GeneratePDF417(ctx, domain.GeneratePDF417Request{Fields: fields})
		if err != nil {
			return domain.BarcodeItem{}, err
		}
		return domain.BarcodeItem{URL: resp.BarcodeURL, Format: resp.Format}, nil
	}
}

// Calculate имитирует POST /api/v1/calculate в BarcodeGen (п.8.2 ТЗ).
// В production: HTTP POST к BARCODEGEN_URL/api/v1/calculate с {revision, field, knownFields}.
func (c *MockClient) Calculate(_ context.Context, revision, field string, knownFields map[string]any) (any, error) {
	switch field {
	case "DAQ":
		fn, _ := knownFields["firstName"].(string)
		ln, _ := knownFields["lastName"].(string)
		return fmt.Sprintf("D%s%s001", string([]rune(fn)[:1]), string([]rune(ln)[:1])), nil
	case "DAK":
		city, _ := knownFields["city"].(string)
		return fmt.Sprintf("HASH_%s_001", city), nil
	case "DBB":
		dob, _ := knownFields["dateOfBirth"].(string)
		return dob, nil
	case "DCS":
		ln, _ := knownFields["lastName"].(string)
		return ln, nil
	case "DCT":
		fn, _ := knownFields["firstName"].(string)
		return fn, nil
	default:
		return fmt.Sprintf("CALC_%s_%s", revision, field), nil
	}
}

// Random имитирует POST /api/v1/random в BarcodeGen (п.8.2 ТЗ).
// В production: HTTP POST к BARCODEGEN_URL/api/v1/random с {revision, field, params}.
func (c *MockClient) Random(_ context.Context, _ string, field string, params map[string]any) (any, error) {
	if paramType, ok := params["type"].(string); ok && paramType == "date" {
		return "2026-01-15", nil
	}
	return fmt.Sprintf("RANDOM_%s", field), nil
}

// GeneratePDF417 имитирует POST /api/v1/generate/pdf417 в BarcodeGen (п.8.2, п.12.3 ТЗ).
// В production: HTTP POST к BARCODEGEN_URL/api/v1/generate/pdf417
//
//	с заголовком X-Idempotency-Key: req.IdempotencyKey
//	и телом {revision, fields, options}.
func (c *MockClient) GeneratePDF417(_ context.Context, req domain.GeneratePDF417Request) (domain.GeneratePDF417Response, error) {
	id := atomic.AddUint64(&c.counter, 1)
	// В production здесь: headers["X-Idempotency-Key"] = req.IdempotencyKey
	if req.IdempotencyKey != "" {
		log.Printf("[barcodegen] pdf417 idempotency-key=%s", req.IdempotencyKey)
	}
	w := req.Options.Width
	if w == 0 {
		w = 400
	}
	h := req.Options.Height
	if h == 0 {
		h = 150
	}
	return domain.GeneratePDF417Response{
		Success:    true,
		BarcodeURL: fmt.Sprintf("https://cdn.example.com/barcodes/pdf417_%d.png", id),
		Format:     "pdf417",
		Metadata: domain.BarcodeMetadata{
			Width:      w,
			Height:     h,
			DataLength: 256,
		},
	}, nil
}

// GenerateCode128 имитирует POST /api/v1/generate/code128 в BarcodeGen (п.8.2, п.12.4 ТЗ).
// В production: HTTP POST к BARCODEGEN_URL/api/v1/generate/code128
//
//	с заголовком X-Idempotency-Key: req.IdempotencyKey
//	и телом {revision: "", fields: {data: req.Data}, options: req.Options}.
func (c *MockClient) GenerateCode128(_ context.Context, req domain.GenerateCode128Request) (domain.GenerateCode128Response, error) {
	id := atomic.AddUint64(&c.counter, 1)
	// В production здесь: headers["X-Idempotency-Key"] = req.IdempotencyKey
	if req.IdempotencyKey != "" {
		log.Printf("[barcodegen] code128 idempotency-key=%s", req.IdempotencyKey)
	}
	w := req.Options.Width
	if w == 0 {
		w = 300
	}
	h := req.Options.Height
	if h == 0 {
		h = 100
	}
	return domain.GenerateCode128Response{
		Success:    true,
		BarcodeURL: fmt.Sprintf("https://cdn.example.com/barcodes/code128_%d.png", id),
		Format:     "code128",
		Metadata: domain.BarcodeMetadata{
			Width:       w,
			Height:      h,
			EncodedData: req.Data,
		},
	}, nil
}
