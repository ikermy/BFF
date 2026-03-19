package timeouts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ikermy/BFF/internal/domain"
	"gopkg.in/yaml.v3"
)

// MemoryStore — in-memory хранилище таймаутов (п.13.2 ТЗ).
// Инициализируется значениями из config (ENV-переменные п.17.1).
// В production заменяется на Redis или персистентное хранилище.
type MemoryStore struct {
	mu      sync.RWMutex
	current domain.ServiceTimeouts
	path    string
}

func NewMemoryStore(barcodeGen, billing, ai, history, auth time.Duration) *MemoryStore {
	return &MemoryStore{
		current: domain.ServiceTimeouts{
			BarcodeGen: int(barcodeGen.Milliseconds()),
			Billing:    int(billing.Milliseconds()),
			AI:         int(ai.Milliseconds()),
			History:    int(history.Milliseconds()),
			Auth:       int(auth.Milliseconds()),
		},
	}
}

func (s *MemoryStore) Get(_ context.Context) (domain.ServiceTimeouts, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current, nil
}

func (s *MemoryStore) Set(_ context.Context, t domain.ServiceTimeouts) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.persistLocked(t); err != nil {
		return err
	}
	s.current = t
	return nil
}

func (s *MemoryStore) LoadFromFile(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.path = path
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create timeouts config dir: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s.persistLocked(s.current)
		}
		return fmt.Errorf("read timeouts config %q: %w", path, err)
	}
	var cfg domain.ServiceTimeouts
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse timeouts config %q: %w", path, err)
	}
	s.current = cfg
	return nil
}

func (s *MemoryStore) persistLocked(t domain.ServiceTimeouts) error {
	if s.path == "" {
		return nil
	}
	payload, err := yaml.Marshal(t)
	if err != nil {
		return fmt.Errorf("marshal timeouts config: %w", err)
	}
	if err := os.WriteFile(s.path, payload, 0o644); err != nil {
		return fmt.Errorf("write timeouts config %q: %w", s.path, err)
	}
	return nil
}
