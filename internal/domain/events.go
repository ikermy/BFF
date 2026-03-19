package domain

// BulkResultEvent — событие bulk.result, публикуемое BFF в Kafka (п.8.3 Bulk_Service_TZ).
type BulkResultEvent struct {
	EventType   string            `json:"eventType"`
	JobID       string            `json:"jobId"`
	BatchID     string            `json:"batchId"`
	Status      string            `json:"status"`
	BuildID     string            `json:"buildId"`
	BarcodeURLs map[string]string `json:"barcodeUrls"`
}

// BarcodeGeneratedEvent — событие barcode.generated, публикуемое после генерации (п.10.3 ТЗ).
type BarcodeGeneratedEvent struct {
	UserID      string         `json:"userId"`
	BuildID     string         `json:"buildId"`
	BatchID     string         `json:"batchId,omitempty"`
	Revision    string         `json:"revision"`
	BarcodeType string         `json:"barcodeType"`
	BarcodeURL  string         `json:"barcodeUrl"`
	Fields      map[string]any `json:"fields"`
	// Billing — разбивка оплаты по источникам (п.10.3 ТЗ).
	Billing   *BarcodeGeneratedBilling `json:"billing,omitempty"`
	CreatedAt string                   `json:"createdAt"`
}

// BarcodeGeneratedBilling — вложенный объект billing в событии barcode.generated.
type BarcodeGeneratedBilling struct {
	TotalCost float64        `json:"totalCost"`
	BySource  QuoteBreakdown `json:"bySource"`
}

// PartialCompletedEvent — событие billing.saga.partial_completed (п.14.4 ТЗ).
// Публикуется когда BarcodeGen упал на части запроса:
// Capture successUnits, Release releasedUnits.
type PartialCompletedEvent struct {
	SagaID        string `json:"sagaId"`
	SuccessUnits  int    `json:"successUnits"`
	ReleasedUnits int    `json:"releasedUnits"`
	Timestamp     string `json:"timestamp"`
}

// BarcodeEditedEvent — событие barcode.edited, публикуется после бесплатного редактирования (п.10.1 ТЗ).
// Consumer (History Service) устанавливает editFlag=true и обновляет imageUrl.
type BarcodeEditedEvent struct {
	ID        string `json:"id"`
	Field     string `json:"field"`
	NewURL    string `json:"newUrl"`
	Timestamp string `json:"timestamp"`
}

// ─── п.11.2 Notifications Service ────────────────────────────────────────────

// NotificationRequest — данные для уведомления об успешной генерации.
// Публикуется в Kafka топик notifications.send (п.11.2 ТЗ).
type NotificationRequest struct {
	UserID       string `json:"userId"`
	BarcodeCount int    `json:"barcodeCount"`
	BuildID      string `json:"buildId"`
	DownloadURL  string `json:"downloadUrl,omitempty"`
}

// ErrorNotificationRequest — данные для уведомления об ошибке генерации.
// Публикуется в Kafka топик notifications.send (п.11.2 ТЗ).
type ErrorNotificationRequest struct {
	UserID  string `json:"userId"`
	Error   string `json:"error"`
	BuildID string `json:"buildId"`
}

// ─── п.11.3 TransHistory Service ─────────────────────────────────────────────

// TransactionType — тип транзакции для TopicHistoryLog (п.11.3 ТЗ).
type TransactionType string

const TransactionGeneration TransactionType = "GENERATION"

// TransactionLog — событие логирования транзакции в History Service.
// Публикуется в Kafka топик trans-history.log (п.11.3 ТЗ).
type TransactionLog struct {
	UserID  string          `json:"userId"`
	Type    TransactionType `json:"type"`
	Amount  float64         `json:"amount,omitempty"`
	Details map[string]any  `json:"details,omitempty"`
}
