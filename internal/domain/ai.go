package domain

// ─── п.9 AI Service ───────────────────────────────────────────────────────────

// AISignatureRequest — запрос на генерацию подписи через AI Service (п.9.2 ТЗ).
// Вызывается когда GenerateRequest.GenerateSignature=true или signatureUrl отсутствует в полях.
type AISignatureRequest struct {
	UserID   string `json:"userId"`
	SagaID   string `json:"sagaId"`
	FullName string `json:"name"`
	Style    string `json:"style"` // "formal" по умолчанию
}

// AISignatureResponse — ответ AI Service на запрос генерации подписи.
type AISignatureResponse struct {
	ImageURL string `json:"imageUrl"`
	Style    string `json:"style"`
}

// AIPhotoRequest — запрос на генерацию фото через AI Service (п.9.2 ТЗ).
// Вызывается когда GenerateRequest.GeneratePhoto=true.
type AIPhotoRequest struct {
	UserID      string `json:"userId"`
	SagaID      string `json:"sagaId"`
	Description string `json:"description"`
	Gender      string `json:"gender,omitempty"`
	Age         int    `json:"age,omitempty"`
}

// AIPhotoResponse — ответ AI Service на запрос генерации фото.
type AIPhotoResponse struct {
	ImageURL string `json:"imageUrl"`
}
