package revisions

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ikermy/BFF/internal/domain"

	"gopkg.in/yaml.v3"
)

// yamlRevision — структура YAML-файла конфигурации ревизии (Приложение A ТЗ).
type yamlRevision struct {
	Name                string          `yaml:"name"`
	DisplayName         string          `yaml:"displayName"`
	Enabled             bool            `yaml:"enabled"`
	RequiredInputFields []string        `yaml:"requiredInputFields"`
	CalculationChain    []yamlChainStep `yaml:"calculationChain"`
	// Schema — секция схемы формы для фронтенда (п.14.5 ТЗ).
	// Если отсутствует в YAML — используется in-memory дефолт из NewMemoryStore.
	Schema *yamlSchema `yaml:"schema,omitempty"`
}

// yamlChainStep — один шаг цепочки в YAML.
type yamlChainStep struct {
	Field     string         `yaml:"field"`
	Source    string         `yaml:"source"`
	DependsOn []string       `yaml:"dependsOn"`
	Params    map[string]any `yaml:"params"`
}

// ─── Schema YAML structs (п.14.5 ТЗ) ─────────────────────────────────────────

type yamlFieldValidation struct {
	MinLength *int   `yaml:"minLength,omitempty"`
	MaxLength *int   `yaml:"maxLength,omitempty"`
	Pattern   string `yaml:"pattern,omitempty"`
	MaxDate   string `yaml:"maxDate,omitempty"`
	MinDate   string `yaml:"minDate,omitempty"`
}

type yamlFieldSchema struct {
	Name       string               `yaml:"name"`
	Type       string               `yaml:"type"`
	Required   bool                 `yaml:"required"`
	Label      string               `yaml:"label"`
	Order      int                  `yaml:"order"`
	Options    []string             `yaml:"options,omitempty"`
	Validation *yamlFieldValidation `yaml:"validation,omitempty"`
}

type yamlFieldGroup struct {
	Name   string   `yaml:"name"`
	Label  string   `yaml:"label"`
	Fields []string `yaml:"fields"`
}

type yamlSchema struct {
	Fields []yamlFieldSchema `yaml:"fields"`
	Groups []yamlFieldGroup  `yaml:"groups,omitempty"`
}

// LoadFromDir загружает revision configs из YAML-файлов каталога в MemoryStore.
func (s *MemoryStore) LoadFromDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create revisions dir %q: %w", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read revisions dir %q: %w", dir, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.dir = dir

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read file %q: %w", path, err)
		}

		var yr yamlRevision
		if err := yaml.Unmarshal(data, &yr); err != nil {
			return fmt.Errorf("parse yaml %q: %w", path, err)
		}

		chain := make([]domain.ChainEntry, 0, len(yr.CalculationChain))
		for _, step := range yr.CalculationChain {
			chain = append(chain, domain.ChainEntry{
				Field:     step.Field,
				Source:    step.Source,
				DependsOn: step.DependsOn,
				Params:    step.Params,
			})
		}

		s.configs[yr.Name] = domain.RevisionConfig{
			Name:                yr.Name,
			DisplayName:         yr.DisplayName,
			Enabled:             yr.Enabled,
			RequiredInputFields: yr.RequiredInputFields,
			CalculationChain:    chain,
		}

		// Загружаем схему, если задана в YAML (п.14.5 ТЗ).
		// Если секции нет — оставляем in-memory дефолт из NewMemoryStore нетронутым.
		if yr.Schema != nil {
			s.schemas[yr.Name] = yamlSchemaToRevisionSchema(yr.Name, yr.DisplayName, yr.Schema)
		}
	}

	return nil
}

// yamlSchemaToRevisionSchema конвертирует YAML-схему в domain.RevisionSchema.
func yamlSchemaToRevisionSchema(revision, displayName string, ys *yamlSchema) domain.RevisionSchema {
	fields := make([]domain.FieldSchema, 0, len(ys.Fields))
	for _, f := range ys.Fields {
		fs := domain.FieldSchema{
			Name:     f.Name,
			Type:     f.Type,
			Required: f.Required,
			Label:    f.Label,
			Order:    f.Order,
			Options:  f.Options,
		}
		if f.Validation != nil {
			fs.Validation = &domain.FieldValidation{
				MinLength: f.Validation.MinLength,
				MaxLength: f.Validation.MaxLength,
				Pattern:   f.Validation.Pattern,
				MaxDate:   f.Validation.MaxDate,
				MinDate:   f.Validation.MinDate,
			}
		}
		fields = append(fields, fs)
	}

	groups := make([]domain.FieldGroup, 0, len(ys.Groups))
	for _, g := range ys.Groups {
		groups = append(groups, domain.FieldGroup{
			Name:   g.Name,
			Label:  g.Label,
			Fields: g.Fields,
		})
	}

	return domain.RevisionSchema{
		Revision:    revision,
		DisplayName: displayName,
		Fields:      fields,
		Groups:      groups,
	}
}

// revisionSchemaToYAML конвертирует domain.RevisionSchema в YAML-структуру для персистентности.
func revisionSchemaToYAML(rs domain.RevisionSchema) *yamlSchema {
	if len(rs.Fields) == 0 {
		return nil
	}
	fields := make([]yamlFieldSchema, 0, len(rs.Fields))
	for _, f := range rs.Fields {
		yf := yamlFieldSchema{
			Name:     f.Name,
			Type:     f.Type,
			Required: f.Required,
			Label:    f.Label,
			Order:    f.Order,
			Options:  f.Options,
		}
		if f.Validation != nil {
			yf.Validation = &yamlFieldValidation{
				MinLength: f.Validation.MinLength,
				MaxLength: f.Validation.MaxLength,
				Pattern:   f.Validation.Pattern,
				MaxDate:   f.Validation.MaxDate,
				MinDate:   f.Validation.MinDate,
			}
		}
		fields = append(fields, yf)
	}

	groups := make([]yamlFieldGroup, 0, len(rs.Groups))
	for _, g := range rs.Groups {
		groups = append(groups, yamlFieldGroup{
			Name:   g.Name,
			Label:  g.Label,
			Fields: g.Fields,
		})
	}

	return &yamlSchema{Fields: fields, Groups: groups}
}
