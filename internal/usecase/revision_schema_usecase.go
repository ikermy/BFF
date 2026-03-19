package usecase

import (
	"context"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
)

// RevisionSchemaUseCase — возвращает схему формы для ревизии (п.14.5 ТЗ).
type RevisionSchemaUseCase struct {
	store ports.RevisionSchemaStore
}

func NewRevisionSchemaUseCase(store ports.RevisionSchemaStore) *RevisionSchemaUseCase {
	return &RevisionSchemaUseCase{store: store}
}

func (u *RevisionSchemaUseCase) Execute(ctx context.Context, revision string) (domain.RevisionSchema, error) {
	if revision == "" {
		return domain.RevisionSchema{}, domain.NewValidationError("revision is required")
	}
	schema, err := u.store.GetSchema(ctx, revision)
	if err != nil {
		return domain.RevisionSchema{}, domain.NewValidationError("revision not found: " + revision)
	}
	return schema, nil
}
