package auth

import (
	"context"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ikermy/BFF/internal/domain"
)

// LocalValidator реализует ports.AuthClient через локальную JWT-валидацию (grpc_kafka_fixes.md §1.1).
// Заменяет HTTPClient, который обращался к Auth Service по HTTP — Auth является gRPC-only сервисом
// и не имеет HTTP-эндпоинта ValidateToken.
// Токен подписан Auth Service тем же JWT_SECRET, который выставлен в env BFF.
type LocalValidator struct {
	secret []byte
}

// NewLocalValidator создаёт валидатор с shared JWT secret.
// secret берётся из cfg.JWTSecret (JWT_SECRET env).
func NewLocalValidator(secret string) *LocalValidator {
	return &LocalValidator{secret: []byte(secret)}
}

// ValidateToken парсит и валидирует JWT локально без обращения к Auth Service.
// Извлекает userId, email, role и permissions из стандартных claims.
func (v *LocalValidator) ValidateToken(_ context.Context, token string) (domain.UserInfo, error) {
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("auth: unexpected signing method: %v", t.Header["alg"])
		}
		return v.secret, nil
	})
	if err != nil {
		return domain.UserInfo{}, fmt.Errorf("auth: invalid token: %w", err)
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || !parsed.Valid {
		return domain.UserInfo{}, fmt.Errorf("auth: invalid token claims")
	}

	userID, _ := claims["userId"].(string)
	if userID == "" {
		// fallback: некоторые реализации Auth используют стандартный claim "sub"
		userID, _ = claims["sub"].(string)
	}
	if userID == "" {
		return domain.UserInfo{}, fmt.Errorf("auth: token missing userId/sub claim")
	}

	email, _ := claims["email"].(string)
	role, _ := claims["role"].(string)

	var permissions []string
	if raw, ok := claims["permissions"].([]interface{}); ok {
		for _, p := range raw {
			if s, ok := p.(string); ok {
				permissions = append(permissions, s)
			}
		}
	}

	return domain.UserInfo{
		UserID:      userID,
		Email:       email,
		Role:        role,
		Permissions: permissions,
	}, nil
}

// GetUserInfo — в текущей архитектуре не используется в HTTP-flows (ValidateToken возвращает полный UserInfo).
// Для Bulk-flows (userID из Kafka) — Auth является gRPC-only, HTTP-вызов невозможен.
// Возвращает минимальный UserInfo на основе userID без обращения к внешним сервисам.
func (v *LocalValidator) GetUserInfo(_ context.Context, userID string) (domain.UserInfo, error) {
	if userID == "" {
		return domain.UserInfo{}, fmt.Errorf("auth: userID is required")
	}
	// Bulk Service передаёт только userID — возвращаем минимальный объект.
	// Если потребуется полный профиль — реализовать gRPC-клиент к Auth Service (auth.proto).
	return domain.UserInfo{
		UserID: userID,
		Role:   "user",
	}, nil
}
