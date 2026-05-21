package idempotency

import (
	"context"
	"sync"
)

// MemoryEditLocker — in-memory реализация EditLocker для dev/test окружения.
// Использует sync.Mutex + map вместо Redis SetNX.
// В production заменяется на RedisEditLocker.
type MemoryEditLocker struct {
	mu    sync.Mutex
	locks map[string]struct{}
}

// NewMemoryEditLocker создаёт in-memory локер.
func NewMemoryEditLocker() *MemoryEditLocker {
	return &MemoryEditLocker{locks: make(map[string]struct{})}
}

// TryLock атомарно захватывает блокировку для barcodeID.
// Возвращает true если лок захвачен, false если уже занят.
func (l *MemoryEditLocker) TryLock(_ context.Context, barcodeID string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.locks[barcodeID]; exists {
		return false, nil
	}
	l.locks[barcodeID] = struct{}{}
	return true, nil
}

// Unlock освобождает блокировку для barcodeID.
func (l *MemoryEditLocker) Unlock(_ context.Context, barcodeID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.locks, barcodeID)
	return nil
}
