package domain

// UserInfo — данные пользователя, полученные от Legacy Auth Service (п.11.1 ТЗ).
type UserInfo struct {
	UserID      string   `json:"userId"`
	Email       string   `json:"email"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions,omitempty"`
}

// AuthTokens — пара токенов, возвращаемая Auth Service при login/register.
type AuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// UserProfile — полный профиль пользователя для фронтенда.
type UserProfile struct {
	UserID             string `json:"userId"`
	Email              string `json:"email"`
	Username           string `json:"username"`
	Nickname           string `json:"nickname"`
	PhotoBase64        string `json:"photoBase64"`
	TelegramUsername   string `json:"telegramUsername"`
	TelegramID         string `json:"telegramId"`
	TelegramPhotoURL   string `json:"telegramPhotoUrl"`
	Origin             string `json:"origin"`
	IsTelegramVerified bool   `json:"isTelegramVerified"`
}

// Credentials — данные для аутентификации (login/register) через Auth Service.
type Credentials struct {
	Email    string
	Password string
	Username string
}

// TelegramAuthData — данные Telegram OAuth, пришедшие от виджета (GET-параметры data-auth-url).
type TelegramAuthData struct {
	TelegramID string `json:"id"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	Username   string `json:"username"`
	PhotoURL   string `json:"photo_url"`
	AuthDate   string `json:"auth_date"`
	Hash       string `json:"hash"`
}
