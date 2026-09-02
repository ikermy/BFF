package auth

import (
	"context"
	"fmt"

	"github.com/ikermy/BFF/internal/adapters/auth/pb"
	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// GRPCClient реализует ports.AuthClient через gRPC к Auth Service (auth.proto).
// Auth является gRPC-only сервисом, поэтому ValidateToken выполняется локально
// (shared JWT secret), а GetUserInfo — через межсервисный метод getPublicProfile
// (метаданные x-service-key), который доступен доверенным сервисам (§11.1 ТЗ).
type GRPCClient struct {
	conn       *grpc.ClientConn
	client     pb.AuthServiceClient
	serviceKey string
	local      *LocalValidator
}

// NewGRPCClient создаёт gRPC-клиент к Auth Service.
// target — адрес auth (например, "auth-service:50051"); serviceKey — SERVICE_API_KEYS;
// jwtSecret — JWT_SECRET для локальной валидации токенов.
func NewGRPCClient(target, serviceKey, jwtSecret string) (*GRPCClient, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("auth: dial grpc: %w", err)
	}
	return &GRPCClient{
		conn:       conn,
		client:     pb.NewAuthServiceClient(conn),
		serviceKey: serviceKey,
		local:      NewLocalValidator(jwtSecret),
	}, nil
}

// Close закрывает gRPC-соединение.
func (c *GRPCClient) Close() error { return c.conn.Close() }

// ValidateToken валидирует JWT локально (симметричный ключ), как LocalValidator.
func (c *GRPCClient) ValidateToken(ctx context.Context, token string) (domain.UserInfo, error) {
	return c.local.ValidateToken(ctx, token)
}

// GetUserInfo запрашивает публичный профиль пользователя через auth.getPublicProfile.
// Межсервисная операция (доверенный сервис) по x-service-key.
func (c *GRPCClient) GetUserInfo(ctx context.Context, userID string) (domain.UserInfo, error) {
	if userID == "" {
		return domain.UserInfo{}, fmt.Errorf("auth: userID is required")
	}

	outCtx := metadata.AppendToOutgoingContext(ctx, "x-service-key", c.serviceKey)
	profile, err := c.client.GetPublicProfile(outCtx, &pb.GetPublicProfileRequest{UserId: userID})
	if err != nil {
		return domain.UserInfo{}, fmt.Errorf("auth: get public profile: %w", err)
	}

	return domain.UserInfo{
		UserID: profile.GetUserId(),
		Role:   "user",
	}, nil
}

// Login аутентифицирует пользователя через auth.Login (gRPC).
func (c *GRPCClient) Login(ctx context.Context, creds domain.Credentials) (domain.AuthTokens, error) {
	resp, err := c.client.Login(ctx, &pb.LoginRequest{
		Email:    creds.Email,
		Password: creds.Password,
	})
	if err != nil {
		return domain.AuthTokens{}, fmt.Errorf("auth: login: %w", err)
	}
	return domain.AuthTokens{
		AccessToken:  resp.GetAccessToken(),
		RefreshToken: resp.GetRefreshToken(),
	}, nil
}

// Register создаёт пользователя через auth.Register (gRPC).
func (c *GRPCClient) Register(ctx context.Context, creds domain.Credentials) (domain.AuthTokens, error) {
	resp, err := c.client.Register(ctx, &pb.RegisterRequest{
		Email:    creds.Email,
		Password: creds.Password,
		Username: creds.Username,
	})
	if err != nil {
		return domain.AuthTokens{}, fmt.Errorf("auth: register: %w", err)
	}
	return domain.AuthTokens{
		AccessToken:  resp.GetAccessToken(),
		RefreshToken: resp.GetRefreshToken(),
	}, nil
}

// TelegramAuth аутентифицирует/регистрирует пользователя через auth.telegramAuth (gRPC).
func (c *GRPCClient) TelegramAuth(ctx context.Context, data domain.TelegramAuthData) (domain.AuthTokens, error) {
	resp, err := c.client.TelegramAuth(ctx, &pb.TelegramAuthRequest{
		TelegramId: data.TelegramID,
		FirstName:  data.FirstName,
		LastName:   data.LastName,
		Username:   data.Username,
		PhotoUrl:   data.PhotoURL,
		AuthDate:   data.AuthDate,
		Hash:       data.Hash,
	})
	if err != nil {
		return domain.AuthTokens{}, fmt.Errorf("auth: telegram auth: %w", err)
	}
	return domain.AuthTokens{
		AccessToken:  resp.GetAccessToken(),
		RefreshToken: resp.GetRefreshToken(),
	}, nil
}

// ChangeAvatar обновляет фотографию профиля через auth.changeAvatar (gRPC).
// accessToken форвардится как User JWT (authorization metadata) для GrpcAuthGuard.
func (c *GRPCClient) ChangeAvatar(ctx context.Context, accessToken, photoBase64 string) error {
	outCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+accessToken)
	_, err := c.client.ChangeAvatar(outCtx, &pb.ChangeAvatarRequest{
		PhotoBase64: photoBase64,
	})
	if err != nil {
		return fmt.Errorf("auth: change avatar: %w", err)
	}
	return nil
}

// ChangeNickname обновляет отображаемое имя (nickname) профиля через auth.changeNickname (gRPC).
// accessToken форвардится как User JWT (authorization metadata) для GrpcAuthGuard.
func (c *GRPCClient) ChangeNickname(ctx context.Context, accessToken, newNickname string) error {
	outCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+accessToken)
	_, err := c.client.ChangeNickname(outCtx, &pb.ChangeNicknameRequest{
		NewNickname: newNickname,
	})
	if err != nil {
		return fmt.Errorf("auth: change nickname: %w", err)
	}
	return nil
}

// ChangeTelegramUsername обновляет Telegram username профиля через auth.changeTelegramUsername (gRPC).
// accessToken форвардится как User JWT (authorization metadata) для GrpcAuthGuard.
func (c *GRPCClient) ChangeTelegramUsername(ctx context.Context, accessToken, telegramUsername string) error {
	outCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+accessToken)
	_, err := c.client.ChangeTelegramUsername(outCtx, &pb.ChangeTelegramUsernameRequest{
		TelegramUsername: telegramUsername,
	})
	if err != nil {
		return fmt.Errorf("auth: change telegram username: %w", err)
	}
	return nil
}

// GetUserProfile возвращает полный профиль пользователя через auth.getUserProfile (gRPC).
// accessToken форвардится как User JWT (authorization metadata) для GrpcAuthGuard.
func (c *GRPCClient) GetUserProfile(ctx context.Context, accessToken string) (domain.UserProfile, error) {
	outCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+accessToken)
	resp, err := c.client.GetUserProfile(outCtx, &pb.GetUserProfileRequest{})
	if err != nil {
		return domain.UserProfile{}, fmt.Errorf("auth: get user profile: %w", err)
	}
	return domain.UserProfile{
		UserID:             resp.GetUserId(),
		Email:              resp.GetEmail(),
		Username:           resp.GetUsername(),
		Nickname:           resp.GetNickname(),
		PhotoBase64:        resp.GetPhotoBase64(),
		TelegramUsername:   resp.GetTelegramUsername(),
		TelegramID:         resp.GetTelegramId(),
		TelegramPhotoURL:   resp.GetTelegramPhotoUrl(),
		Origin:             resp.GetOrigin(),
		IsTelegramVerified: resp.GetIsTelegramVerified(),
	}, nil
}

// LinkEmailToAccount привязывает email к аккаунту через auth.linkEmailToAccount (gRPC).
// accessToken форвардится как User JWT (authorization metadata) для GrpcAuthGuard.
// password может быть пустым (email-only: пароль и origin не меняются).
func (c *GRPCClient) LinkEmailToAccount(ctx context.Context, accessToken, email, password string) error {
	outCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+accessToken)
	_, err := c.client.LinkEmailToAccount(outCtx, &pb.LinkEmailRequest{
		Email:    email,
		Password: password,
	})
	if err != nil {
		return fmt.Errorf("auth: link email: %w", err)
	}
	return nil
}

// ChangeTelegramAccount меняет/обновляет Telegram identity через auth.changeTelegramAccount (gRPC).
// accessToken форвардится как User JWT (authorization metadata) для GrpcAuthGuard.
func (c *GRPCClient) ChangeTelegramAccount(ctx context.Context, accessToken string, data domain.TelegramAuthData) error {
	outCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+accessToken)
	_, err := c.client.ChangeTelegramAccount(outCtx, &pb.ChangeTelegramAccountRequest{
		TelegramId: data.TelegramID,
		FirstName:  data.FirstName,
		LastName:   data.LastName,
		Username:   data.Username,
		PhotoUrl:   data.PhotoURL,
		AuthDate:   data.AuthDate,
		Hash:       data.Hash,
	})
	if err != nil {
		return fmt.Errorf("auth: change telegram account: %w", err)
	}
	return nil
}

// ChangePassword меняет пароль пользователя через auth.changePassword (gRPC).
// accessToken форвардится как User JWT (authorization metadata) для GrpcAuthGuard.
func (c *GRPCClient) ChangePassword(ctx context.Context, accessToken, currentPassword, newPassword string) error {
	outCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+accessToken)
	_, err := c.client.ChangePassword(outCtx, &pb.ChangePasswordRequest{
		CurrentPassword: currentPassword,
		NewPassword:     newPassword,
	})
	if err != nil {
		return fmt.Errorf("auth: change password: %w", err)
	}
	return nil
}

// compile-time check
var _ ports.AuthClient = (*GRPCClient)(nil)
var _ ports.AuthCommandsClient = (*GRPCClient)(nil)
var _ ports.AuthUserCommandsClient = (*GRPCClient)(nil)
