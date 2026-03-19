package ai

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/ikermy/BFF/internal/domain"
)

// MockClient имитирует AI Service (п.9 ТЗ).
// В production заменяется на реальный HTTP-клиент к AI_URL с Service Token.
type MockClient struct {
	counter uint64
}

func NewMockClient() *MockClient {
	return &MockClient{}
}

// GenerateSignature имитирует POST /internal/ai/signature (п.9.2 ТЗ).
// Вызывается когда GenerateRequest.GenerateSignature=true или signatureUrl отсутствует в полях.
// В production: HTTP POST к AI_URL/internal/ai/signature с Bearer Service Token.
func (c *MockClient) GenerateSignature(_ context.Context, req domain.AISignatureRequest) (domain.AISignatureResponse, error) {
	id := atomic.AddUint64(&c.counter, 1)
	style := req.Style
	if style == "" {
		style = "formal"
	}
	return domain.AISignatureResponse{
		ImageURL: fmt.Sprintf("https://cdn.example.com/ai/signature_%d.png", id),
		Style:    style,
	}, nil
}

// GeneratePhoto имитирует POST /internal/ai/photo (п.9.2 ТЗ).
// Вызывается когда GenerateRequest.GeneratePhoto=true.
// В production: HTTP POST к AI_URL/internal/ai/photo с Bearer Service Token.
func (c *MockClient) GeneratePhoto(_ context.Context, req domain.AIPhotoRequest) (domain.AIPhotoResponse, error) {
	id := atomic.AddUint64(&c.counter, 1)
	return domain.AIPhotoResponse{
		ImageURL: fmt.Sprintf("https://cdn.example.com/ai/photo_%s_%d.png", req.Gender, id),
	}, nil
}
