package domain

// KafkaTopic — описание Kafka топика (п.13.3 ТЗ).
type KafkaTopic struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}
