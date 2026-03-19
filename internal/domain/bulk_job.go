package domain

// BulkJobMessage — сообщение из топика bulk.tasks (п.14 ТЗ).
// Публикует Bulk Service, консьюмит BFF worker.
// Одно сообщение содержит массив items для batch-обработки.
type BulkJobMessage struct {
	EventType string        `json:"eventType"`
	BatchID   string        `json:"batchId"`
	UserID    string        `json:"userId"`
	Items     []BulkJobItem `json:"items"`
	Timestamp string        `json:"timestamp"`
	Metadata  BulkJobMeta   `json:"metadata"`
}

// BulkJobItem — одна единица генерации внутри bulk-сообщения.
type BulkJobItem struct {
	JobID                string         `json:"jobId"`
	RowNumber            int            `json:"rowNumber"`
	Revision             string         `json:"revision"`
	Fields               map[string]any `json:"fields"`
	BillingTransactionID string         `json:"billingTransactionId"`
}

// BulkJobMeta — метаданные сообщения bulk.tasks.
type BulkJobMeta struct {
	Source        string `json:"source"`
	CorrelationID string `json:"correlationId"`
}
