package domain

// GenerateRequest — тело запроса POST /api/v1/barcode/generate (п.12.2 ТЗ).
type GenerateRequest struct {
	Revision       string         `json:"revision"`
	BarcodeType    string         `json:"barcodeType"`
	Units          int            `json:"units"`
	Confirmed      bool           `json:"confirmed"`
	BuildID        string         `json:"buildId"`
	BatchID        string         `json:"batchId"`
	Fields         map[string]any `json:"fields"`
	IdempotencyKey string         `json:"-"`

	// AI Service флаги (п.9.3 ТЗ)
	GenerateSignature bool   `json:"generateSignature,omitempty"`
	SignatureStyle    string `json:"signatureStyle,omitempty"`
	GeneratePhoto     bool   `json:"generatePhoto,omitempty"`
	PhotoDescription  string `json:"photoDescription,omitempty"`
	Gender            string `json:"gender,omitempty"`
	Age               int    `json:"age,omitempty"`
}

type BarcodeItem struct {
	URL    string `json:"url"`
	Format string `json:"format"`
}

// GenerateResponse — ответ POST /api/v1/barcode/generate (п.12.2 ТЗ).
// Billing использует GenerateBillingResult, не QuoteResult.
// Computed — поля, рассчитанные ChainExecutor; Skipped — поля, предоставленные пользователем.
type GenerateResponse struct {
	Success  bool                  `json:"success"`
	BuildID  string                `json:"buildId,omitempty"`
	BatchID  string                `json:"batchId,omitempty"`
	Barcodes []BarcodeItem         `json:"barcodes"`
	Computed []string              `json:"computed"`
	Skipped  []string              `json:"skipped"`
	Billing  GenerateBillingResult `json:"billing"`
}

// EditRequest — тело запроса POST /api/v1/barcode/:id/edit (п.10.1 ТЗ).
// Бесплатное редактирование: каждый пользователь имеет право на 1 изменение.
type EditRequest struct {
	Field          string `json:"field" binding:"required"`
	Value          string `json:"value" binding:"required"`
	IdempotencyKey string `json:"-"`
}

// EditResponse — ответ POST /api/v1/barcode/:id/edit (п.10.1 ТЗ).
type EditResponse struct {
	NewURL  string `json:"newUrl,omitempty"`
	CanEdit bool   `json:"canEdit"`
	Reason  string `json:"reason,omitempty"`
}

// ChainResult — результат выполнения цепочки расчётов ChainExecutor (п.4 ТЗ).
type ChainResult struct {
	Fields   map[string]any
	Computed []string
	Skipped  []string
}

// ─── п.10.2 Remake Barcode ────────────────────────────────────────────────────

// BarcodeRecord — данные существующего баркода, возвращаемые GET /api/v1/barcode/:id (п.10.2 ТЗ).
// Используется фронтендом для быстрого заполнения формы при перегенерации.
type BarcodeRecord struct {
	ID          string         `json:"id"`
	UserID      string         `json:"userId"`
	Revision    string         `json:"revision"`
	BarcodeType string         `json:"barcodeType"`
	BarcodeURL  string         `json:"barcodeUrl"`
	Fields      map[string]any `json:"fields"`
	// EditFlag — true если право бесплатного редактирования уже использовано (п.10.1 ТЗ).
	EditFlag  bool   `json:"editFlag"`
	CreatedAt string `json:"createdAt"`
}

// ─── п.12.3 POST /api/v1/barcode/generate/pdf417 ─────────────────────────────

// BarcodeOptions — параметры рендеринга баркода (ширина, высота и т.д.).
type BarcodeOptions struct {
	Width           int    `json:"width,omitempty"`
	Height          int    `json:"height,omitempty"`
	ErrorCorrection string `json:"errorCorrection,omitempty"` // PDF417: L5, M3 и т.д.
	IncludeText     bool   `json:"includeText,omitempty"`     // Code128
}

// GeneratePDF417Request — тело запроса POST /api/v1/barcode/generate/pdf417 (п.12.3 ТЗ).
// IdempotencyKey форвардируется в BarcodeGen как X-Idempotency-Key (п.8.2 ТЗ).
type GeneratePDF417Request struct {
	Revision       string         `json:"revision"`
	BuildID        string         `json:"buildId,omitempty"`
	BatchID        string         `json:"batchId,omitempty"`
	Fields         map[string]any `json:"fields"`
	Options        BarcodeOptions `json:"options,omitempty"`
	IdempotencyKey string         `json:"-"` // из заголовка X-Idempotency-Key, не в теле
}

// BarcodeMetadata — метаданные сгенерированного баркода (п.12.3, 12.4 ТЗ).
type BarcodeMetadata struct {
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	DataLength  int    `json:"dataLength,omitempty"`  // PDF417
	EncodedData string `json:"encodedData,omitempty"` // Code128
}

// GeneratePDF417Response — ответ POST /api/v1/barcode/generate/pdf417 (п.12.3 ТЗ).
type GeneratePDF417Response struct {
	Success    bool            `json:"success"`
	BarcodeURL string          `json:"barcodeUrl"`
	Format     string          `json:"format"`
	Metadata   BarcodeMetadata `json:"metadata"`
}

// ─── п.12.4 POST /api/v1/barcode/generate/code128 ────────────────────────────

// GenerateCode128Request — тело запроса POST /api/v1/barcode/generate/code128 (п.12.4 ТЗ).
// IdempotencyKey форвардируется в BarcodeGen как X-Idempotency-Key (п.8.2 ТЗ).
type GenerateCode128Request struct {
	Data           string         `json:"data" binding:"required"`
	BuildID        string         `json:"buildId,omitempty"`
	Options        BarcodeOptions `json:"options,omitempty"`
	IdempotencyKey string         `json:"-"` // из заголовка X-Idempotency-Key, не в теле
}

// GenerateCode128Response — ответ POST /api/v1/barcode/generate/code128 (п.12.4 ТЗ).
type GenerateCode128Response struct {
	Success    bool            `json:"success"`
	BarcodeURL string          `json:"barcodeUrl"`
	Format     string          `json:"format"`
	Metadata   BarcodeMetadata `json:"metadata"`
}
