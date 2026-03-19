package domain

// UserInfo — данные пользователя, полученные от Legacy Auth Service (п.11.1 ТЗ).
type UserInfo struct {
	UserID      string   `json:"userId"`
	Email       string   `json:"email"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions,omitempty"`
}
