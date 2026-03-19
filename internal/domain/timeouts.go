package domain

// ServiceTimeouts — конфигурация таймаутов для downstream сервисов (п.13.2 ТЗ).
// Значения в миллисекундах — как в ТЗ и ENV-переменных (п.17.1 ТЗ).
// History и Auth опциональны: 0 = клиент использует встроенный дефолт (5s).
type ServiceTimeouts struct {
	BarcodeGen int `json:"barcodeGen"`
	Billing    int `json:"billing"`
	AI         int `json:"ai"`
	History    int `json:"history"`
	Auth       int `json:"auth"`
}
