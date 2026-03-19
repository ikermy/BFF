package domain

// FieldValidation — правила валидации поля формы (п.14.5 ТЗ).
type FieldValidation struct {
	MinLength *int   `json:"minLength,omitempty"`
	MaxLength *int   `json:"maxLength,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
	MaxDate   string `json:"maxDate,omitempty"`
	MinDate   string `json:"minDate,omitempty"`
}

// FieldSchema — описание одного поля формы для фронтенда (п.14.5 ТЗ).
type FieldSchema struct {
	Name       string           `json:"name"`
	Type       string           `json:"type"` // string | date | enum | number
	Required   bool             `json:"required"`
	Label      string           `json:"label"`
	Order      int              `json:"order"`
	Options    []string         `json:"options,omitempty"` // для type=enum
	Validation *FieldValidation `json:"validation,omitempty"`
}

// FieldGroup — группа полей формы (п.14.5 ТЗ).
type FieldGroup struct {
	Name   string   `json:"name"`
	Label  string   `json:"label"`
	Fields []string `json:"fields"`
}

// RevisionSchema — полная схема формы для ревизии (GET /api/v1/revisions/{revision}/schema).
type RevisionSchema struct {
	Revision    string        `json:"revision"`
	DisplayName string        `json:"displayName"`
	Fields      []FieldSchema `json:"fields"`
	Groups      []FieldGroup  `json:"groups"`
}
