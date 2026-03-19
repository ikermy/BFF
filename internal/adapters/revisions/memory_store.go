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

// MemoryStore реализует три порта:
//   - RevisionSchemaStore (п.14.5 ТЗ) — схема формы для фронтенда
//   - RevisionConfigStore (п.13.1 ТЗ) — admin-конфиг (enabled, calculationChain)
type MemoryStore struct {
	schemas map[string]domain.RevisionSchema
	mu      sync.RWMutex
	configs map[string]domain.RevisionConfig
	dir     string
}

func NewMemoryStore() *MemoryStore {
	minLen1, maxLen40 := 1, 40
	return &MemoryStore{
		schemas: map[string]domain.RevisionSchema{
			"US_CA_08292017": {
				Revision:    "US_CA_08292017",
				DisplayName: "California Driver License 2017",
				Fields: []domain.FieldSchema{
					{
						Name: "firstName", Type: "string", Required: true, Label: "First Name", Order: 1,
						Validation: &domain.FieldValidation{MinLength: &minLen1, MaxLength: &maxLen40, Pattern: `^[A-Z][a-zA-Z\s-']+$`},
					},
					{Name: "lastName", Type: "string", Required: true, Label: "Last Name", Order: 2},
					{
						Name: "dateOfBirth", Type: "date", Required: true, Label: "Date of Birth", Order: 3,
						Validation: &domain.FieldValidation{MaxDate: "today-16y"},
					},
					{
						Name: "eyeColor", Type: "enum", Required: true, Label: "Eye Color", Order: 10,
						Options: []string{"BLK", "BLU", "BRO", "GRY", "GRN", "HAZ"},
					},
				},
				Groups: []domain.FieldGroup{
					{Name: "personal", Label: "Personal Information", Fields: []string{"firstName", "lastName", "dateOfBirth"}},
					{Name: "physical", Label: "Physical Description", Fields: []string{"eyeColor", "height", "weight"}},
				},
			},
		},
		configs: map[string]domain.RevisionConfig{
			"US_CA_08292017": {
				Name:                "US_CA_08292017",
				DisplayName:         "California Driver License 2017",
				Enabled:             true,
				RequiredInputFields: []string{"firstName", "lastName", "dateOfBirth"},
				CalculationChain: []domain.ChainEntry{
					{
						Field:     "DAQ",
						Source:    "calculate",
						DependsOn: []string{"firstName", "lastName", "dateOfBirth"},
					},
					{
						Field:     "DAK",
						Source:    "calculate",
						DependsOn: []string{"street", "city", "state", "zipCode"},
					},
					{
						Field:  "DAE",
						Source: "random",
						Params: map[string]any{"type": "date", "range": []int{-30, 0}},
					},
					{
						Field:     "DBB",
						Source:    "calculate",
						DependsOn: []string{"dateOfBirth"},
					},
				},
			},
		},
	}
}

// GetSchema — RevisionSchemaStore (п.14.5 ТЗ).
func (s *MemoryStore) GetSchema(_ context.Context, revision string) (domain.RevisionSchema, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	schema, ok := s.schemas[revision]
	if !ok {
		return domain.RevisionSchema{}, fmt.Errorf("revision %q not found", revision)
	}
	return schema, nil
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
	updated.CalculationChain = req.CalculationChain
	if err := s.persistConfigLocked(updated); err != nil {
		return err
	}
	s.configs[name] = updated
	return nil
}

func (s *MemoryStore) persistDefaultsIfEmptyDirLocked() error {
	if s.dir == "" {
		return nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("read revisions dir %q: %w", s.dir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".yaml" {
			return nil
		}
	}
	for _, cfg := range s.configs {
		if err := s.persistConfigLocked(cfg); err != nil {
			return err
		}
	}
	return nil
}

func (s *MemoryStore) persistConfigLocked(cfg domain.RevisionConfig) error {
	if s.dir == "" {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create revisions dir %q: %w", s.dir, err)
	}
	chain := make([]yamlChainStep, 0, len(cfg.CalculationChain))
	for _, step := range cfg.CalculationChain {
		chain = append(chain, yamlChainStep{
			Field:     step.Field,
			Source:    step.Source,
			DependsOn: step.DependsOn,
			Params:    step.Params,
		})
	}

	// Включаем схему в YAML, если она есть в памяти (п.14.5 ТЗ).
	// Вызывается под s.mu.Lock() — чтение s.schemas безопасно.
	var schema *yamlSchema
	if rs, ok := s.schemas[cfg.Name]; ok {
		schema = revisionSchemaToYAML(rs)
	}

	payload, err := yaml.Marshal(yamlRevision{
		Name:                cfg.Name,
		DisplayName:         cfg.DisplayName,
		Enabled:             cfg.Enabled,
		RequiredInputFields: cfg.RequiredInputFields,
		CalculationChain:    chain,
		Schema:              schema,
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
