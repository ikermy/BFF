package gintransport

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
	"github.com/ikermy/BFF/internal/usecase"

	"github.com/gin-gonic/gin"
)

type APIHandler struct {
	quote          *usecase.QuoteUseCase
	generate       *usecase.GenerateUseCase
	edit           *usecase.EditUseCase
	consumer       ports.BulkJobConsumer
	revisionSchema *usecase.RevisionSchemaUseCase
	revisionStore  ports.RevisionConfigStore
	barcode        ports.BarcodeGenClient
	history        ports.HistoryClient
	authUser       ports.AuthUserCommandsClient
}

func NewAPIHandler(
	quote *usecase.QuoteUseCase,
	generate *usecase.GenerateUseCase,
	edit *usecase.EditUseCase,
	consumer ports.BulkJobConsumer,
	revisionSchema *usecase.RevisionSchemaUseCase,
	revisionStore ports.RevisionConfigStore,
	barcode ports.BarcodeGenClient,
	history ports.HistoryClient,
	authUser ports.AuthUserCommandsClient,
) *APIHandler {
	return &APIHandler{
		quote:          quote,
		generate:       generate,
		edit:           edit,
		consumer:       consumer,
		revisionSchema: revisionSchema,
		revisionStore:  revisionStore,
		barcode:        barcode,
		history:        history,
		authUser:       authUser,
	}
}

// GetQuote — GET /api/v1/billing/quote (п.12.1 ТЗ).
func (h *APIHandler) GetQuote(c *gin.Context) {
	units, err := strconv.Atoi(c.Query("units"))
	if err != nil || units <= 0 {
		RespondError(c, domain.NewValidationError("units must be a positive integer"))
		return
	}

	revision := c.DefaultQuery("revision", "US_CA_08292017")
	userInfo, _ := GetUserInfo(c)

	result, err := h.quote.Execute(c.Request.Context(), userInfo.UserID, units, revision)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// Generate — POST /api/v1/barcode/generate (п.12.2 ТЗ).
func (h *APIHandler) Generate(c *gin.Context) {
	var req domain.GenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError(err.Error()))
		return
	}

	userInfo, _ := GetUserInfo(c)
	req.IdempotencyKey = c.GetHeader("X-Idempotency-Key")

	result, err := h.generate.Execute(c.Request.Context(), userInfo.UserID, req)
	if err != nil {
		// PARTIAL_FUNDS (п.15.1 ТЗ) — не ошибка, HTTP 200 с кодом в теле.
		var appErr *domain.AppError
		if errors.As(err, &appErr) && appErr.Code == domain.ErrCodePartialFunds {
			c.JSON(http.StatusOK, gin.H{
				"code":      domain.ErrCodePartialFunds,
				"message":   appErr.Message,
				"partial":   true,
				"confirmed": false,
			})
			return
		}
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// EditBarcode — POST /api/v1/barcode/:id/edit (п.10.1 ТЗ).
// Бесплатное редактирование: каждый пользователь имеет 1 право на изменение поля.
func (h *APIHandler) EditBarcode(c *gin.Context) {
	barcodeID := c.Param("id")
	if barcodeID == "" {
		RespondError(c, domain.NewValidationError("barcode id is required"))
		return
	}

	var req domain.EditRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError(err.Error()))
		return
	}

	userInfo, _ := GetUserInfo(c)
	req.IdempotencyKey = c.GetHeader("X-Idempotency-Key")
	result, err := h.edit.Execute(c.Request.Context(), userInfo.UserID, barcodeID, req)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetBarcode — GET /api/v1/barcode/:id (п.10.2 ТЗ).
// Возвращает поля существующего баркода для Remake (перегенерации).
// Фронтенд использует ответ для заполнения формы перед вызовом POST /barcode/generate.
func (h *APIHandler) GetBarcode(c *gin.Context) {
	barcodeID := c.Param("id")
	if barcodeID == "" {
		RespondError(c, domain.NewValidationError("barcode id is required"))
		return
	}

	record, err := h.history.GetBarcode(c.Request.Context(), barcodeID)
	if err != nil {
		RespondError(c, domain.NewBarcodeGenError(err))
		return
	}
	c.JSON(http.StatusOK, record)
}

// GetRevisionSchema — GET /api/v1/revisions/:revision/schema (п.14.5 ТЗ).
func (h *APIHandler) GetRevisionSchema(c *gin.Context) {
	revision := c.Param("revision")
	schema, err := h.revisionSchema.Execute(c.Request.Context(), revision)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, schema)
}

// ListRevisions — GET /api/v1/revisions (список доступных ревизий для фронтенда).
func (h *APIHandler) ListRevisions(c *gin.Context) {
	respondRevisionList(c, h.revisionStore)
}

// BulkWake — POST /api/v1/bulk/wake (п.14.6 ТЗ).
// Вызывается Bulk Service (service-token) для проверки что BFF читает Kafka.
func (h *APIHandler) BulkWake(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":          "awake",
		"pendingMessages": h.consumer.PendingCount(),
	})
}

// GeneratePDF417 — POST /api/v1/barcode/generate/pdf417 (п.12.3 ТЗ).
// Dedicated эндпоинт для генерации PDF417 с опциями рендеринга.
// Не проходит через billing — прямой вызов BarcodeGen.
// X-Idempotency-Key из заголовка форвардируется в BarcodeGen (п.8.2 ТЗ).
func (h *APIHandler) GeneratePDF417(c *gin.Context) {
	var req domain.GeneratePDF417Request
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError(err.Error()))
		return
	}
	if len(req.Fields) == 0 {
		RespondError(c, domain.NewValidationError("fields are required"))
		return
	}
	// Инжектируем idempotency key для форвардинга в BarcodeGen (п.8.2 ТЗ)
	req.IdempotencyKey = c.GetHeader("X-Idempotency-Key")

	resp, err := h.barcode.GeneratePDF417(c.Request.Context(), req)
	if err != nil {
		RespondError(c, domain.NewBarcodeGenError(err))
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GenerateCode128 — POST /api/v1/barcode/generate/code128 (п.12.4 ТЗ).
// Dedicated эндпоинт для генерации Code128 с опциями рендеринга.
// Не проходит через billing — прямой вызов BarcodeGen.
// X-Idempotency-Key из заголовка форвардируется в BarcodeGen (п.8.2 ТЗ).
func (h *APIHandler) GenerateCode128(c *gin.Context) {
	var req domain.GenerateCode128Request
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError(err.Error()))
		return
	}
	// Инжектируем idempotency key для форвардинга в BarcodeGen (п.8.2 ТЗ)
	req.IdempotencyKey = c.GetHeader("X-Idempotency-Key")

	resp, err := h.barcode.GenerateCode128(c.Request.Context(), req)
	if err != nil {
		RespondError(c, domain.NewBarcodeGenError(err))
		return
	}
	c.JSON(http.StatusOK, resp)
}

// changeAvatarRequest — тело POST /api/v1/settings/avatar.
type changeAvatarRequest struct {
	PhotoBase64 string `json:"photo" binding:"required"`
}

// ChangeAvatar — POST /api/v1/settings/avatar (защищён User JWT).
// Обновляет фотографию профиля (base64 data URL), проксируя в Auth Service.
func (h *APIHandler) ChangeAvatar(c *gin.Context) {
	if h.authUser == nil {
		RespondError(c, domain.NewBarcodeGenError(fmt.Errorf("auth user service is not configured")))
		return
	}

	var req changeAvatarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError("photo is required"))
		return
	}

	// Берём оригинальный User JWT и форвардим в Auth Service для авторизации.
	authHeader := c.GetHeader("Authorization")
	token := ""
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		token = authHeader[7:]
	}
	if token == "" {
		RespondError(c, domain.NewValidationError("unauthorized"))
		return
	}

	if err := h.authUser.ChangeAvatar(c.Request.Context(), token, req.PhotoBase64); err != nil {
		RespondError(c, domain.NewBarcodeGenError(fmt.Errorf("failed to update avatar: %w", err)))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "photoLen": len(req.PhotoBase64)})
}

// GetUserProfile — GET /api/v1/settings/profile (защищён User JWT).
// Возвращает полный профиль пользователя (email, username, nickname, фото, telegram).
func (h *APIHandler) GetUserProfile(c *gin.Context) {
	if h.authUser == nil {
		RespondError(c, domain.NewBarcodeGenError(fmt.Errorf("auth user service is not configured")))
		return
	}

	authHeader := c.GetHeader("Authorization")
	token := ""
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		token = authHeader[7:]
	}
	if token == "" {
		RespondError(c, domain.NewValidationError("unauthorized"))
		return
	}

	profile, err := h.authUser.GetUserProfile(c.Request.Context(), token)
	if err != nil {
		RespondError(c, domain.NewBarcodeGenError(fmt.Errorf("failed to get user profile: %w", err)))
		return
	}

	c.JSON(http.StatusOK, profile)
}

// changeTelegramUsernameRequest — тело POST /api/v1/settings/telegram.
type changeTelegramUsernameRequest struct {
	TelegramUsername string `json:"telegram" binding:"required"`
}

// ChangeTelegramUsername — POST /api/v1/settings/telegram (защищён User JWT).
// Обновляет Telegram username профиля (без @), проксируя в Auth Service.
func (h *APIHandler) ChangeTelegramUsername(c *gin.Context) {
	if h.authUser == nil {
		RespondError(c, domain.NewBarcodeGenError(fmt.Errorf("auth user service is not configured")))
		return
	}

	var req changeTelegramUsernameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError("telegram is required"))
		return
	}

	authHeader := c.GetHeader("Authorization")
	token := ""
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		token = authHeader[7:]
	}
	if token == "" {
		RespondError(c, domain.NewValidationError("unauthorized"))
		return
	}

	clean := strings.TrimSpace(req.TelegramUsername)
	clean = strings.TrimPrefix(clean, "@")

	if err := h.authUser.ChangeTelegramUsername(c.Request.Context(), token, clean); err != nil {
		RespondError(c, domain.NewBarcodeGenError(fmt.Errorf("failed to update telegram username: %w", err)))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "telegramUsername": clean})
}

// changeNicknameRequest — тело POST /api/v1/settings/nickname.
type changeNicknameRequest struct {
	Nickname string `json:"nickname" binding:"required"`
}

// ChangeNickname — POST /api/v1/settings/nickname (защищён User JWT).
// Обновляет отображаемое имя (nickname) профиля, проксируя в Auth Service.
func (h *APIHandler) ChangeNickname(c *gin.Context) {
	if h.authUser == nil {
		RespondError(c, domain.NewBarcodeGenError(fmt.Errorf("auth user service is not configured")))
		return
	}

	var req changeNicknameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError("nickname is required"))
		return
	}

	authHeader := c.GetHeader("Authorization")
	token := ""
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		token = authHeader[7:]
	}
	if token == "" {
		RespondError(c, domain.NewValidationError("unauthorized"))
		return
	}

	if err := h.authUser.ChangeNickname(c.Request.Context(), token, req.Nickname); err != nil {
		RespondError(c, domain.NewBarcodeGenError(fmt.Errorf("failed to update nickname: %w", err)))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "nickname": req.Nickname})
}

// linkEmailRequest — тело POST /api/v1/settings/email.
// password опционален: если не передан — привязываем только email (email-only).
type linkEmailRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password"`
}

// LinkEmail — POST /api/v1/settings/email (защищён User JWT).
// Привязывает email к аккаунту (в т.ч. для telegram-аккаунтов), проксируя в Auth Service.
func (h *APIHandler) LinkEmail(c *gin.Context) {
	if h.authUser == nil {
		RespondError(c, domain.NewBarcodeGenError(fmt.Errorf("auth user service is not configured")))
		return
	}

	var req linkEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError("email is required"))
		return
	}

	authHeader := c.GetHeader("Authorization")
	token := ""
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		token = authHeader[7:]
	}
	if token == "" {
		RespondError(c, domain.NewValidationError("unauthorized"))
		return
	}

	email := strings.TrimSpace(req.Email)
	if email == "" {
		RespondError(c, domain.NewValidationError("email is required"))
		return
	}

	if err := h.authUser.LinkEmailToAccount(c.Request.Context(), token, email, req.Password); err != nil {
		RespondError(c, domain.NewBarcodeGenError(fmt.Errorf("failed to link email: %w", err)))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "email": email})
}

// changeTelegramAccountRequest — тело POST /api/v1/settings/telegram-account.
// Данные, которые Telegram-виджет прислал на data-auth-url GET-параметрами.
type changeTelegramAccountRequest struct {
	ID        string `json:"id" binding:"required"`
	FirstName string `json:"first_name" binding:"required"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	PhotoURL  string `json:"photo_url"`
	AuthDate  string `json:"auth_date" binding:"required"`
	Hash      string `json:"hash" binding:"required"`
}

// ChangeTelegramAccount — POST /api/v1/settings/telegram-account (защищён User JWT).
// Меняет/обновляет Telegram identity пользователя (свежие данные виджета приоритетны;
// старые данные аудируются в Auth), проксируя в Auth Service.
func (h *APIHandler) ChangeTelegramAccount(c *gin.Context) {
	if h.authUser == nil {
		RespondError(c, domain.NewBarcodeGenError(fmt.Errorf("auth user service is not configured")))
		return
	}

	var req changeTelegramAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError("invalid telegram data"))
		return
	}

	authHeader := c.GetHeader("Authorization")
	token := ""
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		token = authHeader[7:]
	}
	if token == "" {
		RespondError(c, domain.NewValidationError("unauthorized"))
		return
	}

	if err := h.authUser.ChangeTelegramAccount(c.Request.Context(), token, domain.TelegramAuthData{
		TelegramID: req.ID,
		FirstName:  req.FirstName,
		LastName:   req.LastName,
		Username:   req.Username,
		PhotoURL:   req.PhotoURL,
		AuthDate:   req.AuthDate,
		Hash:       req.Hash,
	}); err != nil {
		RespondError(c, domain.NewBarcodeGenError(fmt.Errorf("failed to change telegram account: %w", err)))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// changePasswordRequest — тело POST /api/v1/settings/password.
type changePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required"`
}

// ChangePassword — POST /api/v1/settings/password (защищён User JWT).
// Меняет пароль пользователя (Auth проверяет текущий пароль), проксируя в Auth Service.
func (h *APIHandler) ChangePassword(c *gin.Context) {
	if h.authUser == nil {
		RespondError(c, domain.NewBarcodeGenError(fmt.Errorf("auth user service is not configured")))
		return
	}

	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.NewValidationError("current_password and new_password are required"))
		return
	}

	authHeader := c.GetHeader("Authorization")
	token := ""
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		token = authHeader[7:]
	}
	if token == "" {
		RespondError(c, domain.NewValidationError("unauthorized"))
		return
	}

	if err := h.authUser.ChangePassword(c.Request.Context(), token, req.CurrentPassword, req.NewPassword); err != nil {
		RespondError(c, domain.NewBarcodeGenError(fmt.Errorf("failed to change password: %w", err)))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}
