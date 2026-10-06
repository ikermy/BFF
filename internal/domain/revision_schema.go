package domain

// FieldValidation — правила валидации поля формы (п.14.5 ТЗ).
type FieldValidation struct {
	MinLength *int   `json:"minLength,omitempty"`
	MaxLength *int   `json:"maxLength,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
	MaxDate   string `json:"maxDate,omitempty"`
	MinDate   string `json:"minDate,omitempty"`
}

// FieldOption — typed {value,label} вариант closed-choice поля (ПЛАН §3.1).
type FieldOption struct {
	Value string `json:"value" yaml:"value"`
	Label string `json:"label" yaml:"label"`
}

// FieldSchema — описание одного поля формы для фронтенда (п.14.5 ТЗ).
type FieldSchema struct {
	Name          string           `json:"name"`
	Type          string           `json:"type"` // string | date | enum | number
	Required      bool             `json:"required"`
	Label         string           `json:"label"`
	Order         int              `json:"order"`
	Options       []string         `json:"options,omitempty"`       // для type=enum (значения)
	OptionItems   []FieldOption    `json:"optionItems,omitempty"`   // typed {value,label}
	FallbackValue string           `json:"fallbackValue,omitempty"` // static fallback для closed-choice
	Validation    *FieldValidation `json:"validation,omitempty"`
}

// FieldGroup — группа полей формы (п.14.5 ТЗ).
type FieldGroup struct {
	Name   string   `json:"name"`
	Label  string   `json:"label"`
	Fields []string `json:"fields"`
}

// RevisionSchema — полная схема формы для ревизии (GET /api/v1/revisions/{revision}/schema).
// Дополнительно (ПЛАН §3.1) отдаёт capability/effectiveDate/baseInput/generatedFields
// из того же статического профиля.
type RevisionSchema struct {
	Revision              string        `json:"revision"`
	DisplayName           string        `json:"displayName"`
	RevisionEffectiveDate string        `json:"revisionEffectiveDate,omitempty"`
	SupportedModes        []string      `json:"supportedModes,omitempty"`
	BaseInput             []string      `json:"baseInput,omitempty"`
	GeneratedFields       []string      `json:"generatedFields,omitempty"`
	Fields                []FieldSchema `json:"fields"`
	Groups                []FieldGroup  `json:"groups"`
}
