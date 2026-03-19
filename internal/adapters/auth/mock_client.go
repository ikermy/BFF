package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/ikermy/BFF/internal/domain"
)

// MockClient имитирует Legacy Auth Service.
// В production заменяется на реальный HTTP-клиент к AUTH_URL.
type MockClient struct{}

func NewMockClient() *MockClient {
	return &MockClient{}
}

// ValidateToken принимает любой непустой Bearer-токен и возвращает тестового пользователя.
// Токен вида "invalid-*" имитирует отказ аутентификации.
func (c *MockClient) ValidateToken(_ context.Context, token string) (domain.UserInfo, error) {
	if token == "" {
		return domain.UserInfo{}, fmt.Errorf("empty token")
	}
	if strings.HasPrefix(token, "invalid-") {
		return domain.UserInfo{}, fmt.Errorf("token rejected by auth service")
	}

	// В реальной реализации здесь HTTP POST к AUTH_URL/api/v1/validate
	return domain.UserInfo{
		UserID:      "mock-user-id",
		Email:       "user@example.com",
		Role:        "user",
		Permissions: []string{"barcode:generate", "barcode:edit"},
	}, nil
}

// GetUserInfo возвращает полную информацию о пользователе по его ID (п.11.1 ТЗ).
// В production здесь HTTP GET к AUTH_URL/api/v1/users/{userId}.
func (c *MockClient) GetUserInfo(_ context.Context, userID string) (domain.UserInfo, error) {
	if userID == "" {
		return domain.UserInfo{}, fmt.Errorf("userID is required")
	}
	return domain.UserInfo{
		UserID:      userID,
		Email:       fmt.Sprintf("%s@example.com", userID),
		Role:        "user",
		Permissions: []string{"barcode:generate", "barcode:edit"},
	}, nil
}
