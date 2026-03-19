package idempotency

import (
	"context"
	"sync"
	"time"
)

type entry struct {
	body      []byte // nil = in-flight (зарезервирован, ответ ещё не готов)
	expiresAt time.Time
}

// MemoryStore — in-memory IdempotencyStore с TTL (п.14.1 ТЗ).
// В production заменяется на Redis:
//
//	Reserve  → SET idempotency:{key} "" EX {ttl} NX
//	Set      → SET idempotency:{key} {body} EX {ttl}
//	Get      → GET idempotency:{key}
type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string]entry
	ttl     time.Duration
}

func NewMemoryStore(ttl time.Duration) *MemoryStore {
	s := &MemoryStore{
		entries: make(map[string]entry),
		ttl:     ttl,
	}
	go s.cleanup()
	return s
}

// Get возвращает (body, true, nil) если ключ завершён с готовым ответом.
// Возвращает (nil, false, nil) если ключ не найден, просрочен или in-flight.
func (s *MemoryStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[key]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false, nil
	}
	if e.body == nil {
		// Ключ зарезервирован, но ответ ещё не сохранён (in-flight)
		return nil, false, nil
	}
	return e.body, true, nil
}

// Reserve резервирует ключ (SetNX) — соответствует checkOrSet(key, null) из ТЗ.
// Возвращает true если ключ успешно зарезервирован (первый запрос).
// Возвращает false если ключ уже существует (параллельный или повторный запрос).
func (s *MemoryStore) Reserve(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if ok && !time.Now().After(e.expiresAt) {
		// Ключ уже существует и не просрочен — не резервируем
		return false, nil
	}
	// Записываем in-flight маркер с nil body
	s.entries[key] = entry{body: nil, expiresAt: time.Now().Add(s.ttl)}
	return true, nil
}

// Set сохраняет готовый ответ (перезаписывает in-flight маркер).
// Соответствует checkOrSet(key, response) из ТЗ.
func (s *MemoryStore) Set(_ context.Context, key string, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = entry{body: body, expiresAt: time.Now().Add(s.ttl)}
	return nil
}

// cleanup удаляет просроченные записи каждые 5 минут.
func (s *MemoryStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		for k, e := range s.entries {
			if now.After(e.expiresAt) {
				delete(s.entries, k)
			}
		}
		s.mu.Unlock()
	}
}
