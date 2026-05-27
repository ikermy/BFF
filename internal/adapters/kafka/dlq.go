package kafka

import "time"

// DLQRecord — инфраструктурный конверт для Dead Letter Queue (bulk.tasks.dlq).
// Публикуется при невалидном JSON или ошибке хендлера в Consumer.Start().
// Содержит оригинальный payload (RawValue) — сообщение можно переиграть 1-в-1
// без потери данных (replay через akhq / kafkacat / replay-воркер).
type DLQRecord struct {
	OriginalTopic string    `json:"originalTopic"`
	Partition     int       `json:"partition"`
	Offset        int64     `json:"offset"`
	RawValue      []byte    `json:"rawValue"`    // оригинальный payload as-is
	ErrorReason   string    `json:"errorReason"` // "unmarshal" | "handler"
	ErrorDetail   string    `json:"errorDetail"` // err.Error()
	FailedAt      time.Time `json:"failedAt"`
}
