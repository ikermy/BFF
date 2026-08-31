package gintransport

import (
	"strings"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"

	"github.com/gin-gonic/gin"
)

// AuthHandler — публичные auth-операции (login/register), проксируемые в Auth Service.
// Не требуют User JWT. Фронтенд обращается к ним через Envoy → BFF.
type AuthHandler struct {
	auth ports.AuthCommandsClient
}

// NewAuthHandler создаёт хендлер публичных auth-команд.
func NewAuthHandler(auth ports.AuthCommandsClient) *AuthHandler {
	return &AuthHandler{auth: auth}
}

// authLoginRequest — тело POST /api/v1/auth/login.
type authLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// authRegisterRequest — тело POST /api/v1/auth/register.
type authRegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Username string `json:"username"`
}

// authTokensResponse — тело ответа login/register.
type authTokensResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// telegramAuthRequest — тело POST /api/v1/auth/telegram.
// Это данные, которые Telegram-виджет прислал бы на data-auth-url GET-параметрами.
type telegramAuthRequest struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	PhotoURL  string `json:"photo_url"`
	AuthDate  string `json:"auth_date"`
	Hash      string `json:"hash"`
}

// Login — POST /api/v1/auth/login (публичный).
func (h *AuthHandler) Login(c *gin.Context) {
	var req authLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError("invalid request body"))
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		RespondError(c, domain.NewValidationError("email and password are required"))
		return
	}

	tokens, err := h.auth.Login(c.Request.Context(), domain.Credentials{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		// 401 UNAUTHORIZED — неверные учётные данные.
		c.JSON(401, ErrorResponse{Code: "UNAUTHORIZED", Message: "invalid credentials"})
		return
	}

	c.JSON(200, authTokensResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
	})
}

// TelegramAuth — POST /api/v1/auth/telegram (публичный).
// Проксирует данные Telegram OAuth в Auth Service (auth.telegramAuth), который
// проверяет HMAC-подпись, находит/регистрирует пользователя и возвращает токены.
func (h *AuthHandler) TelegramAuth(c *gin.Context) {
	var req telegramAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError("invalid request body"))
		return
	}

	if req.ID == "" || req.FirstName == "" || req.AuthDate == "" || req.Hash == "" {
		RespondError(c, domain.NewValidationError("id, first_name, auth_date and hash are required"))
		return
	}

	tokens, err := h.auth.TelegramAuth(c.Request.Context(), domain.TelegramAuthData{
		TelegramID: req.ID,
		FirstName:  req.FirstName,
		LastName:   req.LastName,
		Username:   req.Username,
		PhotoURL:   req.PhotoURL,
		AuthDate:   req.AuthDate,
		Hash:       req.Hash,
	})
	if err != nil {
		c.JSON(401, ErrorResponse{Code: "UNAUTHORIZED", Message: "invalid telegram authentication data"})
		return
	}

	c.JSON(200, authTokensResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
	})
}

// Register — POST /api/v1/auth/register (публичный).
func (h *AuthHandler) Register(c *gin.Context) {
	var req authRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError("invalid request body"))
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	req.Username = strings.TrimSpace(req.Username)
	if req.Email == "" || req.Password == "" || req.Username == "" {
		RespondError(c, domain.NewValidationError("email, password and username are required"))
		return
	}

	tokens, err := h.auth.Register(c.Request.Context(), domain.Credentials{
		Email:    req.Email,
		Password: req.Password,
		Username: req.Username,
	})
	if err != nil {
		// 409 CONFLICT — email уже занят, либо внутренняя ошибка Auth.
		c.JSON(409, ErrorResponse{Code: "REGISTRATION_FAILED", Message: "registration failed"})
		return
	}

	c.JSON(200, authTokensResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
	})
}
