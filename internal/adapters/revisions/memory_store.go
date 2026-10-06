package revisions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ikermy/BFF/internal/domain"
	"gopkg.in/yaml.v3"
)

// MemoryStore реализует два порта поверх ОДНОГО объекта-профиля
// (domain.RevisionConfig, ПЛАН §3.1):
//   - RevisionSchemaStore (п.14.5 ТЗ) — схема формы для фронтенда (ToSchema)
//   - RevisionConfigStore (п.13.1 ТЗ) — admin-конфиг (enabled)
//
// Определения профилей хранятся только в configs: единственный source of truth —
// version-controlled YAML в configs/revisions (LoadFromDir). NewMemoryStore()
// возвращает пустой store.
type MemoryStore struct {
	mu      sync.RWMutex
	configs map[string]domain.RevisionConfig
	dir     string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		configs: make(map[string]domain.RevisionConfig),
	}
}

// GetSchema — RevisionSchemaStore (п.14.5 ТЗ). Строится из того же profile
// object, что и GetConfig (единое слияние definition + schema).
func (s *MemoryStore) GetSchema(_ context.Context, revision string) (domain.RevisionSchema, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, ok := s.configs[revision]
	if !ok {
		return domain.RevisionSchema{}, fmt.Errorf("revision %q not found", revision)
	}
	return cfg.ToSchema(), nil
}

// ListConfigs — RevisionConfigStore (п.13.1 ТЗ).
func (s *MemoryStore) ListConfigs(_ context.Context) ([]domain.RevisionConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]domain.RevisionConfig, 0, len(s.configs))
	for _, cfg := range s.configs {
		list = append(list, cfg)
	}
	return list, nil
}

// GetConfig — RevisionConfigStore (п.13.1 ТЗ).
func (s *MemoryStore) GetConfig(_ context.Context, name string) (domain.RevisionConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, ok := s.configs[name]
	if !ok {
		return domain.RevisionConfig{}, fmt.Errorf("revision %q not found", name)
	}
	return cfg, nil
}

// UpdateConfig — RevisionConfigStore (п.13.1 ТЗ).
func (s *MemoryStore) UpdateConfig(_ context.Context, name string, req domain.UpdateRevisionRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, ok := s.configs[name]
	if !ok {
		return fmt.Errorf("revision %q not found", name)
	}
	updated := cfg
	updated.Enabled = req.Enabled
	// ПЛАН §3.1: admin не изменяет статический definition (steps/fields).
	if err := s.persistConfigLocked(updated); err != nil {
		return err
	}
	s.configs[name] = updated
	return nil
}

func (s *MemoryStore) persistConfigLocked(cfg domain.RevisionConfig) error {
	if s.dir == "" {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create revisions dir %q: %w", s.dir, err)
	}

	steps := make([]yamlGenerationStep, 0, len(cfg.GenerationSteps))
	for _, st := range cfg.GenerationSteps {
		steps = append(steps, yamlGenerationStep{
			ID:       st.ID,
			Endpoint: st.Endpoint,
			Input:    st.Input,
			Output:   st.Output,
		})
	}

	// Схема — часть того же profile object (ПЛАН §3.1).
	schema := revisionSchemaToYAML(domain.RevisionSchema{Fields: cfg.Fields, Groups: cfg.Groups})

	var limits *yamlLimits
	if cfg.Limits.MaxUnits != 0 {
		limits = &yamlLimits{MaxUnits: cfg.Limits.MaxUnits}
	}

	payload, err := yaml.Marshal(yamlRevision{
		Name:                  cfg.Name,
		Version:               cfg.Version,
		DisplayName:           cfg.DisplayName,
		Enabled:               cfg.Enabled,
		RequiredInputFields:   cfg.RequiredInputFields,
		BaseInput:             cfg.BaseInput,
		GenerationSteps:       steps,
		RevisionEffectiveDate: cfg.RevisionEffectiveDate,
		SupportedModes:        cfg.SupportedModes,
		RenderAllowlist:       cfg.RenderAllowlist,
		RequiredRenderFields:  cfg.RequiredRenderFields,
		Defaults:              cfg.Defaults,
		Country:               cfg.Country,
		HeightCm:              cfg.HeightCm,
		Limits:                limits,
		Schema:                schema,
	})
	if err != nil {
		return fmt.Errorf("marshal revision %q: %w", cfg.Name, err)
	}
	path := filepath.Join(s.dir, cfg.Name+".yaml")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write revision %q: %w", path, err)
	}
	return nil
}
