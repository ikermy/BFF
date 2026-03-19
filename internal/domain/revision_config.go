package domain

// ChainEntry — один шаг цепочки вычислений ревизии (п.4, п.13.1 ТЗ).
type ChainEntry struct {
	Field     string         `json:"field"`
	Source    string         `json:"source"`              // "calculate" | "user" | "random"
	DependsOn []string       `json:"dependsOn,omitempty"` // поля, необходимые для расчёта
	Params    map[string]any `json:"params,omitempty"`    // параметры для source=random
}

// RevisionConfig — admin-конфигурация ревизии (п.13.1 ТЗ).
// Отличается от RevisionSchema (п.14.5): схема — для фронтенда,
// конфиг — для управления enabled и calculationChain.
type RevisionConfig struct {
	Name                string       `json:"name"`
	DisplayName         string       `json:"displayName"`
	Enabled             bool         `json:"enabled"`
	RequiredInputFields []string     `json:"requiredInputFields,omitempty"` // минимальный набор (п.5.2 ТЗ)
	CalculationChain    []ChainEntry `json:"-"`                             // хранится как объекты
}

// UpdateRevisionRequest — тело PUT /admin/revisions/{revision} (п.13.1 ТЗ).
type UpdateRevisionRequest struct {
	Enabled          bool         `json:"enabled"`
	CalculationChain []ChainEntry `json:"calculationChain"`
}
