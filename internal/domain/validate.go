package domain

// InternalValidateRequest — запрос валидации строки от Bulk Service (п.7.1 Bulk_Service_TZ).
type InternalValidateRequest struct {
	UserID    string         `json:"userId"`
	BatchID   string         `json:"batchId"`
	BuildID   string         `json:"buildId"`
	RowNumber int            `json:"rowNumber"`
	Revision  string         `json:"revision"`
	Fields    map[string]any `json:"fields"`
}

// InternalValidateResponse — результат валидации строки.
type InternalValidateResponse struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}
