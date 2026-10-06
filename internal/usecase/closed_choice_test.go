package usecase

import (
	"testing"

	"github.com/ikermy/BFF/internal/domain"
)

func TestNormalizeEngineFields(t *testing.T) {
	got := domain.NormalizeEngineFields(map[string]any{
		"issueDate": "03072022", "auditCode": "A1", "DAJ": "CA",
	})
	if got["DBD"] != "03072022" || got["DCJ"] != "A1" || got["DAJ"] != "CA" {
		t.Fatalf("got %v", got)
	}
	// existing engine key wins
	got2 := domain.NormalizeEngineFields(map[string]any{"issueDate": "x", "DBD": "y"})
	if got2["DBD"] != "y" {
		t.Fatalf("engine key must win, got %v", got2["DBD"])
	}
}

func TestValidateClosedChoices(t *testing.T) {
	schema := domain.RevisionSchema{
		Revision: "X",
		Fields: []domain.FieldSchema{
			{Name: "eyeColor", Type: "enum", Options: []string{"BLK", "BRO"}},
			{Name: "firstName", Type: "string"},
		},
	}
	if appErr := validateClosedChoices(schema, map[string]any{"eyeColor": "BRO"}); appErr != nil {
		t.Fatalf("valid choice rejected: %+v", appErr)
	}
	if appErr := validateClosedChoices(schema, map[string]any{"eyeColor": "XYZ"}); appErr == nil {
		t.Fatal("expected invalid closed-choice error")
	}
	if appErr := validateClosedChoices(schema, map[string]any{}); appErr != nil {
		t.Fatalf("absent optional field must pass: %+v", appErr)
	}
}

func TestApplyChoiceFallbacks(t *testing.T) {
	schema := domain.RevisionSchema{
		Revision: "X",
		Fields: []domain.FieldSchema{
			{Name: "vehicleClass", Type: "enum", Options: []string{"A", "C"}, FallbackValue: "C"},
			{Name: "restrictions", Type: "enum", Options: []string{"NONE", "01"}}, // без fallback
			{Name: "eyeColor", Type: "string", Options: []string{"BLK"}, FallbackValue: "BLK"},
		},
	}
	fields := map[string]any{"firstName": "JOHN"}
	applyChoiceFallbacks(schema.Fields, fields)

	if fields["vehicleClass"] != "C" {
		t.Fatalf("expected fallback C, got %v", fields["vehicleClass"])
	}
	if _, ok := fields["restrictions"]; ok {
		t.Fatalf("field without fallback must stay absent, got %v", fields["restrictions"])
	}
	if fields["firstName"] != "JOHN" {
		t.Fatalf("non-choice field must be untouched, got %v", fields["firstName"])
	}

	// уже заполненное значение не перезаписывается.
	fields2 := map[string]any{"vehicleClass": "A"}
	applyChoiceFallbacks(schema.Fields, fields2)
	if fields2["vehicleClass"] != "A" {
		t.Fatalf("user value must win, got %v", fields2["vehicleClass"])
	}
}
