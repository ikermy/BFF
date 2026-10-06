package barcodegen

import (
	"context"

	"github.com/ikermy/BFF/internal/domain"
)

// WithUserID кладёт userID в контекст (ownerId для internal render).
// Делегирует в domain, чтобы usecase мог ставить ownerId без импорта адаптера.
func WithUserID(ctx context.Context, userID string) context.Context {
	return domain.WithUserID(ctx, userID)
}

// UserIDFromContext извлекает userID из контекста.
func UserIDFromContext(ctx context.Context) (string, bool) {
	return domain.UserIDFromContext(ctx)
}
