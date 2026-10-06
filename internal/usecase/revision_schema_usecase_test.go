package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/ikermy/BFF/internal/adapters/revisions/revisionstest"
	"github.com/ikermy/BFF/internal/domain"
)

func TestRevisionSchemaUseCase_EmptyRevision(t *testing.T) {
	uc := NewRevisionSchemaUseCase(revisionstest.MustLoad(t))

	_, err := uc.Execute(context.Background(), "")
	if err == nil {
		t.Fatal("expected validation error")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %s", appErr.Code)
	}
}

func TestRevisionSchemaUseCase_UnknownRevision(t *testing.T) {
	uc := NewRevisionSchemaUseCase(revisionstest.MustLoad(t))

	_, err := uc.Execute(context.Background(), "UNKNOWN")
	if err == nil {
		t.Fatal("expected validation error for unknown revision")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.Code != domain.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %s", appErr.Code)
	}
}

func TestRevisionSchemaUseCase_Success(t *testing.T) {
	uc := NewRevisionSchemaUseCase(revisionstest.MustLoad(t))

	schema, err := uc.Execute(context.Background(), "US_CA_08292017")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if schema.Revision != "US_CA_08292017" {
		t.Fatalf("expected revision US_CA_08292017, got %s", schema.Revision)
	}
	if len(schema.Fields) == 0 {
		t.Fatal("expected non-empty schema fields")
	}
}

// TestRevisionSchemaUseCase_EnrichedFromSingleProfileObject — ПЛАН §3.1: без
// отдельного config-store схема обогащается из того же profile object.
func TestRevisionSchemaUseCase_EnrichedFromSingleProfileObject(t *testing.T) {
	uc := NewRevisionSchemaUseCase(revisionstest.MustLoad(t))

	schema, err := uc.Execute(context.Background(), "US_CA_08292017")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if schema.RevisionEffectiveDate != "2017-08-29" {
		t.Fatalf("revisionEffectiveDate = %q", schema.RevisionEffectiveDate)
	}
	if len(schema.SupportedModes) == 0 {
		t.Fatal("supportedModes must be populated")
	}
	if len(schema.GeneratedFields) == 0 {
		t.Fatal("generatedFields must be populated")
	}
	if len(schema.Groups) == 0 {
		t.Fatal("schema groups must be populated from the same object")
	}
	base := make(map[string]bool, len(schema.BaseInput))
	for _, f := range schema.BaseInput {
		base[f] = true
	}
	if !base["firstName"] || !base["dateOfBirth"] {
		t.Fatalf("baseInput must include required fields, got %v", schema.BaseInput)
	}
}
