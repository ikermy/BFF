package topupbonus

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ikermy/BFF/internal/domain"
	"gopkg.in/yaml.v3"
)

// MemoryStore — in-memory хранилище конфига бонусов (п.14.7 ТЗ).
// В production заменяется на реализацию с персистентностью (Redis / БД).
type MemoryStore struct {
	mu   sync.RWMutex
	cfg  domain.TopupBonusConfig
	path string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		cfg: domain.TopupBonusConfig{
			Enabled: false,
			Tiers:   []domain.TopupBonusTier{},
		},
	}
}

func (s *MemoryStore) Get(_ context.Context) (domain.TopupBonusConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg, nil
}

func (s *MemoryStore) Set(_ context.Context, cfg domain.TopupBonusConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.persistLocked(cfg); err != nil {
		return err
	}
	s.cfg = cfg
	return nil
}

func (s *MemoryStore) LoadFromFile(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.path = path
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create topup-bonus config dir: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s.persistLocked(s.cfg)
		}
		return fmt.Errorf("read topup-bonus config %q: %w", path, err)
	}
	var cfg domain.TopupBonusConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse topup-bonus config %q: %w", path, err)
	}
	s.cfg = cfg
	return nil
}

func (s *MemoryStore) persistLocked(cfg domain.TopupBonusConfig) error {
	if s.path == "" {
		return nil
	}
	payload, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal topup-bonus config: %w", err)
	}
	if err := os.WriteFile(s.path, payload, 0o644); err != nil {
		return fmt.Errorf("write topup-bonus config %q: %w", s.path, err)
	}
	return nil
}
