package domain

// ChainEntry — legacy per-field шаг цепочки (п.4, п.13.1 ТЗ). Runtime больше не
// исполняет CalculationChain: единственная модель генерации — GenerationStep.
// Тип сохранён только для обратной совместимости admin-запроса (поле принимается,
// но игнорируется — static definition не перезаписывается admin-ом).
type ChainEntry struct {
	Field     string         `json:"field"`
	Source    string         `json:"source"`              // "calculate" | "user" | "random"
	DependsOn []string       `json:"dependsOn,omitempty"` // поля, необходимые для расчёта
	Params    map[string]any `json:"params,omitempty"`    // параметры для source=random
}

// GenerationStep — сгруппированный шаг генерации (ПЛАН §3.1): один HTTP-вызов
// derive-ручки BarcodeGen с входом и выходом.
type GenerationStep struct {
	ID       string   `json:"id" yaml:"id"`
	Endpoint string   `json:"endpoint" yaml:"endpoint"` // "random" | "calculate"
	Input    []string `json:"input" yaml:"input"`
	Output   []string `json:"output" yaml:"output"`
}

// RevisionConfig — единый static definition профиля (ПЛАН §3.1/§3.2): и
// admin-конфигурация (п.13.1 ТЗ), и форма для фронтенда (п.14.5 ТЗ) хранятся
// в одном объекте. Schema-endpoint (`GetSchema`) строится из этого же объекта,
// отдельного schema-store больше нет.
//
// Поля definition:
//   - GenerationSteps — grouped derive steps (единственная модель генерации);
//   - RevisionEffectiveDate — ISO дата вступления ревизии (нижняя граница issueDate);
//   - SupportedModes — "prepare" | "auto" | "prepared";
//   - RenderAllowlist — какие поля разрешено передавать в render;
//   - Fields/Groups — схема формы (не сериализуется в admin-JSON, json:"-").
type RevisionConfig struct {
	Name                  string           `json:"name"`
	Version               string           `json:"version,omitempty"` // версия статического профиля
	DisplayName           string           `json:"displayName"`
	Enabled               bool             `json:"enabled"`
	RequiredInputFields   []string         `json:"requiredInputFields,omitempty"` // минимальный набор (п.5.2 ТЗ)
	BaseInput             []string         `json:"baseInput,omitempty"`           // engine-поля (DOB→DBB); пусто → = RequiredInputFields
	GenerationSteps       []GenerationStep `json:"generationSteps,omitempty"`
	RevisionEffectiveDate string           `json:"revisionEffectiveDate,omitempty"`
	SupportedModes        []string         `json:"supportedModes,omitempty"`
	RenderAllowlist       []string         `json:"renderAllowlist,omitempty"`
	RequiredRenderFields  []string         `json:"requiredRenderFields,omitempty"`
	// Defaults — static engine-константы профиля (QQQ/DCA/DBC/…), подставляются
	// в render-values, если поле не задано пользователем/derive.
	Defaults map[string]string `json:"defaults,omitempty"`
	Limits   Limits            `json:"limits,omitempty"`

	// Country — "US" | "CA": влияет на кодирование дат перед render
	// (US → MMDDYYYY, CA → YYYYMMDD, doc BFF_ПОЛЯ...).
	Country string `json:"country,omitempty"`
	// HeightCm — рост в UI (дюймы) перед render переводится в см (ON/AB).
	HeightCm bool `json:"heightCm,omitempty"`

	// Fields/Groups — схема формы (п.14.5). Часть единого profile object;
	// не попадает в admin-JSON, отдаётся через RevisionSchemaStore.GetSchema.
	Fields []FieldSchema `json:"-"`
	Groups []FieldGroup  `json:"-"`
}

// GeneratedFields — union output всех grouped generation steps профиля.
func (c RevisionConfig) GeneratedFields() []string {
	seen := make(map[string]bool)
	var out []string
	for _, st := range c.GenerationSteps {
		for _, o := range st.Output {
			if !seen[o] {
				seen[o] = true
				out = append(out, o)
			}
		}
	}
	return out
}

// ToSchema строит API-представление схемы из единого profile object (ПЛАН §3.1):
// те же definition-поля, что использует runtime, без отдельного schema-store.
func (c RevisionConfig) ToSchema() RevisionSchema {
	base := c.BaseInput
	if len(base) == 0 {
		base = c.RequiredInputFields
	}
	schema := RevisionSchema{
		Revision:              c.Name,
		DisplayName:           c.DisplayName,
		RevisionEffectiveDate: c.RevisionEffectiveDate,
		SupportedModes:        c.SupportedModes,
		BaseInput:             base,
		GeneratedFields:       c.GeneratedFields(),
		Fields:                c.Fields,
		Groups:                c.Groups,
	}
	if schema.Fields == nil {
		schema.Fields = []FieldSchema{}
	}
	if schema.Groups == nil {
		schema.Groups = []FieldGroup{}
	}
	return schema
}

// Limits — лимиты профиля (ПЛАН §3.1). MaxUnits=0 — без ограничения.
type Limits struct {
	MaxUnits int `json:"maxUnits,omitempty" yaml:"maxUnits"`
}

// UpdateRevisionRequest — тело PUT /admin/revisions/{revision} (п.13.1 ТЗ).
// CalculationChain принимается для обратной совместимости, но игнорируется:
// admin меняет только Enabled, static definition не перезаписывается.
type UpdateRevisionRequest struct {
	Enabled          bool         `json:"enabled"`
	CalculationChain []ChainEntry `json:"calculationChain"`
}
