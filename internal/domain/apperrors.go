package domain

import "fmt"

// Коды ошибок по п.15.1 ТЗ.
const (
	ErrCodeValidation        = "VALIDATION_ERROR"   // 400
	ErrCodeMissingDependency = "MISSING_DEPENDENCY" // 400
	ErrCodeInsufficientFunds = "INSUFFICIENT_FUNDS" // 402
	ErrCodePartialFunds      = "PARTIAL_FUNDS"      // 200 — не ошибка, успешный ответ
	ErrCodeDuplicateRequest  = "DUPLICATE_REQUEST"  // 200 — не ошибка, возвращаем кэш
	ErrCodeBarcodeGenError   = "BARCODEGEN_ERROR"   // 503
	ErrCodeBillingError      = "BILLING_ERROR"      // 503
)

// AppError — структурированная ошибка BFF (п.15.1 ТЗ).
// Реализует интерфейс error. Хендлер извлекает HTTPStatus через errors.As.
// Details — дополнительные поля в теле ответа (например, topUpRequired для 402).
type AppError struct {
	Code       string         `json:"code"`
	HTTPStatus int            `json:"-"`
	Message    string         `json:"message"`
	Details    map[string]any `json:"details,omitempty"`
}

func (e *AppError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Конструкторы — по одному на каждый код ошибки из п.15.1 ТЗ.

func NewValidationError(msg string) *AppError {
	return &AppError{Code: ErrCodeValidation, HTTPStatus: 400, Message: msg}
}

func NewMissingDependencyError(field string, deps []string) *AppError {
	return &AppError{
		Code:       ErrCodeMissingDependency,
		HTTPStatus: 400,
		Message:    fmt.Sprintf("field %q requires: %v", field, deps),
	}
}

func NewBarcodeGenError(cause error) *AppError {
	return &AppError{
		Code:       ErrCodeBarcodeGenError,
		HTTPStatus: 503,
		Message:    cause.Error(),
	}
}

func NewBillingError(cause error) *AppError {
	return &AppError{
		Code:       ErrCodeBillingError,
		HTTPStatus: 503,
		Message:    cause.Error(),
	}
}

// NewRequiredFieldsError — ошибка минимального набора (п.5.2 ТЗ).
// Возвращает VALIDATION_ERROR 400 со списком обязательных полей в details.
func NewRequiredFieldsError(missing []string) *AppError {
	return &AppError{
		Code:       ErrCodeValidation,
		HTTPStatus: 400,
		Message:    fmt.Sprintf("required input fields missing: %v", missing),
		Details:    map[string]any{"missingFields": missing},
	}
}
