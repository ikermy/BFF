package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/ikermy/BFF/internal/adapters/revisions"
	"github.com/ikermy/BFF/internal/domain"
)

func TestRevisionSchemaUseCase_EmptyRevision(t *testing.T) {
	uc := NewRevisionSchemaUseCase(revisions.NewMemoryStore())

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
	uc := NewRevisionSchemaUseCase(revisions.NewMemoryStore())

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
	uc := NewRevisionSchemaUseCase(revisions.NewMemoryStore())

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
