package history

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ikermy/BFF/internal/domain"
)

// MockClient имитирует History Service (п.10.1, п.10.2 ТЗ).
// В production заменяется на реальный HTTP-клиент к HISTORY_URL.
type MockClient struct {
	mu        sync.RWMutex
	editFlags map[string]bool                 // barcodeID → editFlag (true = уже использовано)
	records   map[string]domain.BarcodeRecord // barcodeID → запись баркода
}

func NewMockClient() *MockClient {
	return &MockClient{
		editFlags: make(map[string]bool),
		records:   make(map[string]domain.BarcodeRecord),
	}
}

// CheckFreeEdit вызывает GET /internal/barcode/:id/check-free-edit (п.10.1 ТЗ).
// Возвращает canEdit=true если editFlag=false (первое редактирование ещё не использовано).
func (c *MockClient) CheckFreeEdit(_ context.Context, barcodeID string) (bool, error) {
	if barcodeID == "" {
		return false, fmt.Errorf("barcodeID is required")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	used := c.editFlags[barcodeID]
	return !used, nil // canEdit = !editFlag
}

// GetBarcode возвращает данные баркода для Remake (п.10.2 ТЗ).
// В production: HTTP GET к HISTORY_URL/internal/barcode/:id.
func (c *MockClient) GetBarcode(_ context.Context, barcodeID string) (domain.BarcodeRecord, error) {
	if barcodeID == "" {
		return domain.BarcodeRecord{}, fmt.Errorf("barcodeID is required")
	}
	c.mu.RLock()
	rec, ok := c.records[barcodeID]
	editFlag := c.editFlags[barcodeID]
	c.mu.RUnlock()

	if !ok {
		// Если запись не найдена — возвращаем mock-данные для тестов
		return domain.BarcodeRecord{
			ID:          barcodeID,
			UserID:      "mock-user-id",
			Revision:    "US_CA_08292017",
			BarcodeType: "pdf417",
			BarcodeURL:  fmt.Sprintf("https://cdn.example.com/barcodes/%s.png", barcodeID),
			Fields: map[string]any{
				"firstName":   "JOHN",
				"lastName":    "DOE",
				"dateOfBirth": "1990-05-15",
			},
			EditFlag:  editFlag,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	rec.EditFlag = editFlag
	return rec, nil
}

// AddRecord сохраняет запись баркода в mock-хранилище.
// Используется из тестов и GenerateUseCase (имитация сохранения в History).
func (c *MockClient) AddRecord(rec domain.BarcodeRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records[rec.ID] = rec
}

// SetEditFlag — вспомогательный метод для тестов.
// В production editFlag устанавливается Consumer'ом топика barcode.edited.
func (c *MockClient) SetEditFlag(barcodeID string, used bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.editFlags[barcodeID] = used
}
