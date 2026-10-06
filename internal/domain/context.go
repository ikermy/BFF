package domain

import "context"

type contextKey int

const ctxKeyUserID contextKey = iota

// WithUserID кладёт authenticated userID в контекст (ownerId для internal render).
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ctxKeyUserID, userID)
}

// UserIDFromContext извлекает userID из контекста.
func UserIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKeyUserID).(string)
	return id, ok && id != ""
}
