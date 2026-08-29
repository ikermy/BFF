package gintransport

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/ikermy/BFF/internal/adapters/barcodegen"
	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/metrics"
	"github.com/ikermy/BFF/internal/ports"

	"github.com/gin-gonic/gin"
)

const ContextKeyUserInfo = "userInfo"

// MaintenanceModeMiddleware — возвращает 503 когда MAINTENANCE_MODE=true (п.19 ТЗ).
// Применяется глобально перед всеми маршрутами.
func MaintenanceModeMiddleware(enabled bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enabled {
			c.Next()
			return
		}
		// /health и /metrics всегда доступны даже в maintenance
		if c.FullPath() == "/health" || c.FullPath() == "/metrics" {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"error": "service is under maintenance, please try again later",
			"code":  "MAINTENANCE_MODE",
		})
	}
}

// MetricsMiddleware — инкрементирует bff_requests_total (п.17 ТЗ).
func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		status := "success"
		if c.Writer.Status() >= 400 {
			status = "error"
		}
		metrics.RequestsTotal.WithLabelValues(c.FullPath(), status).Inc()
	}
}

// UserJWTMiddleware валидирует User JWT через Legacy Auth Service (п.16.1 ТЗ).
// Если ENABLE_LEGACY_AUTH=false — пропускает проверку (п.15 ТЗ).
func UserJWTMiddleware(auth ports.AuthClient, enableLegacyAuth bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enableLegacyAuth {
			// Feature flag отключён — пропускаем аутентификацию
			c.Set(ContextKeyUserInfo, domain.UserInfo{UserID: "anonymous"})
			c.Next()
			return
		}

		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}

		token := strings.TrimPrefix(header, "Bearer ")
		userInfo, err := auth.ValidateToken(c.Request.Context(), token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		c.Set(ContextKeyUserInfo, userInfo)
		// userID прокидывается в request-контекст — нужен legacy-адаптеру BarcodeGen
		// для минта сервисного JWT (порт не расширяем, см. отчёт §4.2 п.1).
		c.Request = c.Request.WithContext(barcodegen.WithUserID(c.Request.Context(), userInfo.UserID))
		c.Next()
	}
}

// GetUserInfo извлекает UserInfo из контекста gin (устанавливается UserJWTMiddleware).
func GetUserInfo(c *gin.Context) (domain.UserInfo, bool) {
	val, exists := c.Get(ContextKeyUserInfo)
	if !exists {
		return domain.UserInfo{}, false
	}
	info, ok := val.(domain.UserInfo)
	return info, ok
}

// ServiceJWTMiddleware проверяет Service Token для /internal/* (п.16.1 ТЗ).
// Сравнение через subtle.ConstantTimeCompare предотвращает Timing Attack (BFF_Final_Status_Report.md п.4).
func ServiceJWTMiddleware(expectedToken string) gin.HandlerFunc {
	expected := []byte(expectedToken)
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), expected) != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "invalid service token"})
			return
		}
		c.Next()
	}
}

// AdminJWTMiddleware проверяет Admin JWT для /admin/* (п.16.1 ТЗ).
// Сравнение через subtle.ConstantTimeCompare предотвращает Timing Attack (BFF_Final_Status_Report.md п.4).
func AdminJWTMiddleware(expectedToken string) gin.HandlerFunc {
	expected := []byte(expectedToken)
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), expected) != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "invalid admin token"})
			return
		}
		c.Next()
	}
}
