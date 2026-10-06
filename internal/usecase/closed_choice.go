package usecase

import (
	"strings"

	"github.com/ikermy/BFF/internal/domain"
)

// applyChoiceFallbacks заполняет пустые closed-choice поля их static fallbackValue
// (ПЛАН §3.1/§3.5). Не трогает поля, уже заполненные пользователем или chain.
func applyChoiceFallbacks(fields []domain.FieldSchema, values map[string]any) {
	for _, f := range fields {
		if f.FallbackValue == "" || len(f.Options) == 0 {
			continue
		}
		if !hasUserValue(values, f.Name) {
			values[f.Name] = f.FallbackValue
		}
	}
}

// validateClosedChoicesFromFields проверяет значения closed-choice полей против
// static options единого profile object (ПЛАН §3.5). Ошибка → VALIDATION_ERROR 400.
func validateClosedChoicesFromFields(fields []domain.FieldSchema, values map[string]any) *domain.AppError {
	var invalid []string
	for _, f := range fields {
		if len(f.Options) == 0 {
			continue
		}
		raw, ok := values[f.Name]
		if !ok || raw == nil {
			continue
		}
		value, _ := raw.(string)
		if !containsFold(f.Options, value) {
			invalid = append(invalid, f.Name)
		}
	}
	if len(invalid) == 0 {
		return nil
	}
	return domain.NewValidationError("invalid closed-choice values: " + strings.Join(invalid, ", "))
}

// validateClosedChoices — обёртка для совместимости (schema → fields).
func validateClosedChoices(schema domain.RevisionSchema, values map[string]any) *domain.AppError {
	return validateClosedChoicesFromFields(schema.Fields, values)
}

// applyChoiceFallbacksSchema — обёртка для совместимости (schema → fields).
func applyChoiceFallbacksSchema(schema domain.RevisionSchema, values map[string]any) {
	applyChoiceFallbacks(schema.Fields, values)
}

func containsFold(options []string, value string) bool {
	for _, o := range options {
		if strings.EqualFold(o, value) {
			return true
		}
	}
	return false
}
