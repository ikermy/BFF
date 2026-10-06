package usecase

import (
	"context"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
)

// PrepareUseCase — stateless подготовка редактируемого черновика (ПЛАН §3.3):
// profile lookup → grouped chain-executor → полный draftFields.
// Без Billing, render и History.
type PrepareUseCase struct {
	chain     *ChainExecutor
	revisions ports.RevisionConfigStore
	schemas   ports.RevisionSchemaStore
}

func NewPrepareUseCase(chain *ChainExecutor, revisions ports.RevisionConfigStore, schemas ports.RevisionSchemaStore) *PrepareUseCase {
	return &PrepareUseCase{chain: chain, revisions: revisions, schemas: schemas}
}

// Execute выполняет prepare: проверяет профиль и его capability "prepare",
// запускает цепочку и возвращает черновик.
func (u *PrepareUseCase) Execute(ctx context.Context, req domain.PrepareRequest) (domain.PrepareResponse, error) {
	cfg, err := u.revisions.GetConfig(ctx, req.Revision)
	if err != nil {
		return domain.PrepareResponse{}, domain.NewValidationError("revision not found: " + req.Revision)
	}
	if !supportsMode(cfg.SupportedModes, "prepare") {
		return domain.PrepareResponse{}, domain.NewValidationError("prepare mode is not supported by profile: " + req.Revision)
	}

	fields := req.Fields
	if fields == nil {
		fields = map[string]any{}
	}

	result, err := u.chain.ExecuteGrouped(ctx, req.Revision, fields)
	if err != nil {
		return domain.PrepareResponse{}, err
	}

	// closed-choice: пустые enum-поля получают static fallbackValue, значения
	// проверяются по static options профиля.
	if u.schemas != nil {
		if schema, sErr := u.schemas.GetSchema(ctx, req.Revision); sErr == nil {
			applyChoiceFallbacks(schema.Fields, result.Fields)
			if appErr := validateClosedChoicesFromFields(schema.Fields, result.Fields); appErr != nil {
				return domain.PrepareResponse{}, appErr
			}
		}
	}

	return domain.PrepareResponse{
		Success:     true,
		Revision:    req.Revision,
		DraftFields: result.Fields,
		Computed:    result.Computed,
		Skipped:     result.Skipped,
	}, nil
}

// supportsMode возвращает true, если профиль не ограничивает modes (пусто) или
// явно содержит запрошенный.
func supportsMode(modes []string, want string) bool {
	if len(modes) == 0 {
		return true
	}
	for _, m := range modes {
		if m == want {
			return true
		}
	}
	return false
}
