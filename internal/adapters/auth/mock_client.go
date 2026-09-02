package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
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

// Login имитирует аутентификацию и возвращает тестовые токены (для dev, без Auth Service).
func (c *MockClient) Login(_ context.Context, creds domain.Credentials) (domain.AuthTokens, error) {
	if creds.Email == "" || creds.Password == "" {
		return domain.AuthTokens{}, fmt.Errorf("email and password are required")
	}
	return domain.AuthTokens{
		AccessToken:  "mock-access-token",
		RefreshToken: "mock-refresh-token",
	}, nil
}

// Register имитирует регистрацию и возвращает тестовые токены (для dev, без Auth Service).
func (c *MockClient) Register(_ context.Context, creds domain.Credentials) (domain.AuthTokens, error) {
	if creds.Email == "" || creds.Password == "" || creds.Username == "" {
		return domain.AuthTokens{}, fmt.Errorf("email, password and username are required")
	}
	return domain.AuthTokens{
		AccessToken:  "mock-access-token",
		RefreshToken: "mock-refresh-token",
	}, nil
}

// TelegramAuth имитирует вход/регистрацию через Telegram (для dev, без Auth Service).
func (c *MockClient) TelegramAuth(_ context.Context, _ domain.TelegramAuthData) (domain.AuthTokens, error) {
	return domain.AuthTokens{
		AccessToken:  "mock-tg-access-token",
		RefreshToken: "mock-tg-refresh-token",
	}, nil
}

// ChangeAvatar имитирует обновление фотографии профиля (для dev, без Auth Service).
func (c *MockClient) ChangeAvatar(_ context.Context, _ string, photoBase64 string) error {
	if photoBase64 != "" && len(photoBase64) > 1_500_000 {
		return fmt.Errorf("photo is too large")
	}
	return nil
}

// ChangeTelegramUsername имитирует обновление Telegram username (для dev, без Auth Service).
func (c *MockClient) ChangeTelegramUsername(_ context.Context, _ string, telegramUsername string) error {
	return nil
}

// ChangeNickname имитирует обновление отображаемого имени (для dev, без Auth Service).
func (c *MockClient) ChangeNickname(_ context.Context, _ string, _ string) error {
	return nil
}

// GetUserProfile имитирует получение профиля (для dev, без Auth Service).
func (c *MockClient) GetUserProfile(_ context.Context, _ string) (domain.UserProfile, error) {
	return domain.UserProfile{
		UserID:   "mock-user-1",
		Email:    "mock@example.com",
		Username: "mockuser",
	}, nil
}

// LinkEmailToAccount имитирует привязку email к аккаунту (для dev, без Auth Service).
func (c *MockClient) LinkEmailToAccount(_ context.Context, _ string, email, _ string) error {
	if email == "" {
		return fmt.Errorf("email is required")
	}
	return nil
}

// ChangeTelegramAccount имитирует изменение Telegram identity (для dev, без Auth Service).
func (c *MockClient) ChangeTelegramAccount(_ context.Context, _ string, data domain.TelegramAuthData) error {
	if data.TelegramID == "" || data.FirstName == "" {
		return fmt.Errorf("telegram id and first name are required")
	}
	return nil
}

// ChangePassword имитирует смену пароля (для dev, без Auth Service).
func (c *MockClient) ChangePassword(_ context.Context, _ string, currentPassword, _ string) error {
	if currentPassword == "" {
		return fmt.Errorf("current password is required")
	}
	return nil
}

// compile-time check
var _ ports.AuthCommandsClient = (*MockClient)(nil)
var _ ports.AuthUserCommandsClient = (*MockClient)(nil)
