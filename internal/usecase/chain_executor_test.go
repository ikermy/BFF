package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/ikermy/BFF/internal/adapters/barcodegen"
	"github.com/ikermy/BFF/internal/adapters/revisions"
	"github.com/ikermy/BFF/internal/domain"
)

// containsStr — вспомогательная функция для поиска строки в срезе.
func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func TestChainExecutor_DoesNotOverwriteUserInput(t *testing.T) {
	// п.4.2 ТЗ: пользовательский ввод НИКОГДА не перезаписывается!
	store := revisions.NewMemoryStore()
	exec := NewChainExecutor(barcodegen.NewMockClient(), store)

	userInput := map[string]any{
		"firstName":   "JOHN",
		"lastName":    "DOE",
		"dateOfBirth": "1990-05-15",
		"street":      "123 Main St",
		"city":        "Los Angeles",
		"state":       "CA",
		"zipCode":     "90001",
		"DAQ":         "USER_PROVIDED_VALUE", // пользователь уже заполнил!
	}

	result, err := exec.Execute(context.Background(), "US_CA_08292017", userInput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// DAQ должен остаться как есть
	if result.Fields["DAQ"] != "USER_PROVIDED_VALUE" {
		t.Errorf("expected DAQ=%q, got %q", "USER_PROVIDED_VALUE", result.Fields["DAQ"])
	}
	// DAQ должен быть в Skipped
	found := false
	for _, s := range result.Skipped {
		if s == "DAQ" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected DAQ in Skipped, got Skipped=%v", result.Skipped)
	}
	// DAQ не должен быть в Computed
	for _, c := range result.Computed {
		if c == "DAQ" {
			t.Errorf("DAQ must not be in Computed")
		}
	}
}

func TestChainExecutor_ComputesMissingFields(t *testing.T) {
	store := revisions.NewMemoryStore()
	exec := NewChainExecutor(barcodegen.NewMockClient(), store)

	userInput := map[string]any{
		"firstName":   "JOHN",
		"lastName":    "DOE",
		"dateOfBirth": "1990-05-15",
		"street":      "123 Main St",
		"city":        "Los Angeles",
		"state":       "CA",
		"zipCode":     "90001",
	}

	result, err := exec.Execute(context.Background(), "US_CA_08292017", userInput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// DAQ должен быть вычислен (зависимости присутствуют)
	if result.Fields["DAQ"] == nil || result.Fields["DAQ"] == "" {
		t.Errorf("expected DAQ to be computed, got nil/empty")
	}
	found := false
	for _, c := range result.Computed {
		if c == "DAQ" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected DAQ in Computed, got %v", result.Computed)
	}
	// DAE должен быть вычислен (source=random)
	if result.Fields["DAE"] == nil || result.Fields["DAE"] == "" {
		t.Errorf("expected DAE to be computed, got nil/empty")
	}
}

func TestChainExecutor_MissingDependencies(t *testing.T) {
	// DAK требует street/city/state/zipCode — если их нет, возвращаем MISSING_DEPENDENCY
	store := revisions.NewMemoryStore()
	exec := NewChainExecutor(barcodegen.NewMockClient(), store)

	userInput := map[string]any{
		"firstName":   "JOHN",
		"lastName":    "DOE",
		"dateOfBirth": "1990-05-15",
		// address fields отсутствуют
	}

	_, err := exec.Execute(context.Background(), "US_CA_08292017", userInput)
	if err == nil {
		t.Fatal("expected MISSING_DEPENDENCY error")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeMissingDependency || appErr.HTTPStatus != 400 {
		t.Fatalf("expected MISSING_DEPENDENCY 400, got %s %d", appErr.Code, appErr.HTTPStatus)
	}
}

func TestChainExecutor_InvalidRevision(t *testing.T) {
	store := revisions.NewMemoryStore()
	exec := NewChainExecutor(barcodegen.NewMockClient(), store)

	_, err := exec.Execute(context.Background(), "NON_EXISTENT_REVISION", map[string]any{})
	if err == nil {
		t.Fatal("expected error for unknown revision")
	}
}

func TestChainExecutor_PreservesUserFieldsInResult(t *testing.T) {
	// Все пользовательские поля должны присутствовать в result.Fields
	store := revisions.NewMemoryStore()
	exec := NewChainExecutor(barcodegen.NewMockClient(), store)

	userInput := map[string]any{
		"firstName":   "JANE",
		"lastName":    "SMITH",
		"dateOfBirth": "1985-03-20",
		"street":      "123 Main St",
		"city":        "Los Angeles",
		"state":       "CA",
		"zipCode":     "90001",
		"eyeColor":    "BLU",
	}

	result, err := exec.Execute(context.Background(), "US_CA_08292017", userInput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for k, v := range userInput {
		if result.Fields[k] != v {
			t.Errorf("field %q: expected %v, got %v", k, v, result.Fields[k])
		}
	}
}

// TestChainExecutor_Section4_3_FullExample — полный пример п.4.3 ТЗ.
// DAQ заполнен пользователем → Skipped. DAK/DAE/DBB → Computed.
// ВАЖНО: street обязателен для DAK (dependsOn: [street, city, state, zipCode]).
// Если street отсутствует — DAK попадает в Skipped (soft-skip, не ошибка).
func TestChainExecutor_Section4_3_FullExample(t *testing.T) {
	store := revisions.NewMemoryStore()
	exec := NewChainExecutor(barcodegen.NewMockClient(), store)

	// Полный набор: DAQ заполнен пользователем + все адресные поля включая street
	userInput := map[string]any{
		"firstName":   "JOHN",
		"lastName":    "DOE",
		"dateOfBirth": "1990-05-15",
		"DAQ":         "D1234567", // ← пользователь УЖЕ заполнил!
		"street":      "123 Main St",
		"city":        "Los Angeles",
		"state":       "CA",
		"zipCode":     "90001",
	}

	result, err := exec.Execute(context.Background(), "US_CA_08292017", userInput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// DAQ: сохранён как есть (пользователь заполнил)
	if result.Fields["DAQ"] != "D1234567" {
		t.Errorf("DAQ: expected D1234567 (user value), got %v", result.Fields["DAQ"])
	}
	// DAQ → Skipped, NOT Computed
	if !containsStr(result.Skipped, "DAQ") {
		t.Errorf("DAQ must be in Skipped, got Skipped=%v", result.Skipped)
	}
	if containsStr(result.Computed, "DAQ") {
		t.Errorf("DAQ must NOT be in Computed, got Computed=%v", result.Computed)
	}

	// DAK: вычислен (все зависимости есть: street, city, state, zipCode)
	if result.Fields["DAK"] == nil || result.Fields["DAK"] == "" {
		t.Errorf("DAK: expected computed value, got %v", result.Fields["DAK"])
	}
	// DAE: вычислен (source=random, без dependsOn)
	if result.Fields["DAE"] == nil || result.Fields["DAE"] == "" {
		t.Errorf("DAE: expected random value, got %v", result.Fields["DAE"])
	}
	// DBB: вычислен (dateOfBirth присутствует)
	if result.Fields["DBB"] == nil || result.Fields["DBB"] == "" {
		t.Errorf("DBB: expected computed value, got %v", result.Fields["DBB"])
	}
	for _, field := range []string{"DAK", "DAE", "DBB"} {
		if !containsStr(result.Computed, field) {
			t.Errorf("%s must be in Computed, got Computed=%v", field, result.Computed)
		}
	}
	// Пользовательские поля сохранены без изменений
	for k, v := range userInput {
		if result.Fields[k] != v {
			t.Errorf("user field %q: expected %v, got %v", k, v, result.Fields[k])
		}
	}
}

// TestChainExecutor_Section4_3_MissingStreet — вариант п.4.3 без street.
// По ТЗ отсутствие dependsOn должно приводить к MISSING_DEPENDENCY.
func TestChainExecutor_Section4_3_MissingStreet(t *testing.T) {
	store := revisions.NewMemoryStore()
	exec := NewChainExecutor(barcodegen.NewMockClient(), store)

	// Точно как в п.4.3 ТЗ — без street
	userInput := map[string]any{
		"firstName":   "JOHN",
		"lastName":    "DOE",
		"dateOfBirth": "1990-05-15",
		"DAQ":         "D1234567",
		"city":        "Los Angeles",
		"state":       "CA",
		"zipCode":     "90001",
		// street отсутствует!
	}

	_, err := exec.Execute(context.Background(), "US_CA_08292017", userInput)
	if err == nil {
		t.Fatal("expected MISSING_DEPENDENCY when street is absent")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeMissingDependency {
		t.Fatalf("expected %s, got %s", domain.ErrCodeMissingDependency, appErr.Code)
	}
}
