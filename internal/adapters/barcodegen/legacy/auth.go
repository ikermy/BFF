package legacy

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Сервисный JWT-минтер для BarcodeGen.
//
// BarcodeGen ставит AuthGuard('jwt') на все ручки и читает из токена payload.id,
// payload.role, payload.isBanned (отчёт §4.2 п.1). Адаптер минтит токен с claims
// {id, role:"user"} — так userId в Prisma-записях BarcodeGen остаётся корректным,
// а BFF не таскает пользовательский токен во внутренний периметр.
//
// isBanned не задаём — истина по банам живёт в Auth/BFF-периметре.

// tokenTTL — время жизни сервисного токена BarcodeGen.
const tokenTTL = 15 * time.Minute

// tokenMinter подписывает сервисные JWT общим секретом (JWT_ACCESS_SECRET).
type tokenMinter struct {
	secret []byte
	ttl    time.Duration
}

func newTokenMinter(secret string) *tokenMinter {
	if secret == "" {
		secret = "dev-jwt-secret"
	}
	return &tokenMinter{secret: []byte(secret), ttl: tokenTTL}
}

// Mint создаёт JWT для заданного userID.
func (m *tokenMinter) Mint(userID string) (string, error) {
	if userID == "" {
		userID = "anonymous"
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"id":   userID,
		"role": "user",
		"iat":  now.Unix(),
		"exp":  now.Add(m.ttl).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}
