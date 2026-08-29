package barcodegen

import "context"

// contextKey — private тип ключа контекста, чтобы избежать коллизий.
type contextKey int

// ctxKeyUserID — ключ, под которым userID попадает в request-контекст.
// Устанавливается в UserJWTMiddleware и читается legacy-адаптером для минта
// сервисного JWT (порт не расширяем — userID берём из context, см. отчёт §4.2 п.1).
const ctxKeyUserID contextKey = iota

// WithUserID возвращает копию ctx с сохранённым userID.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ctxKeyUserID, userID)
}

// UserIDFromContext извлекает userID из контекста. Возвращает (userID, true),
// если значение задано и не пустое.
func UserIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKeyUserID).(string)
	return id, ok && id != ""
}
