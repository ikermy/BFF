package revisions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ikermy/BFF/internal/domain"

	"gopkg.in/yaml.v3"
)

// validateRevision — fail-fast проверка профиля (ПЛАН §3.1): имя, ISO
// revisionEffectiveDate, форма grouped generationSteps, supportedModes.
func validateRevision(yr yamlRevision) error {
	if strings.TrimSpace(yr.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if yr.RevisionEffectiveDate != "" {
		if _, err := time.Parse("2006-01-02", yr.RevisionEffectiveDate); err != nil {
			return fmt.Errorf("revisionEffectiveDate must be ISO YYYY-MM-DD: %w", err)
		}
	}
	seen := make(map[string]bool)
	allOutputs := make(map[string]bool)
	for _, st := range yr.GenerationSteps {
		for _, out := range st.Output {
			allOutputs[out] = true
		}
	}
	produced := make(map[string]bool)
	for i, st := range yr.GenerationSteps {
		if strings.TrimSpace(st.ID) == "" {
			return fmt.Errorf("generationSteps[%d].id is required", i)
		}
		if st.Endpoint != "random" && st.Endpoint != "calculate" {
			return fmt.Errorf("generationSteps[%d].endpoint must be random|calculate", i)
		}
		if len(st.Input) == 0 {
			return fmt.Errorf("generationSteps[%d].input is empty", i)
		}
		if len(st.Output) == 0 {
			return fmt.Errorf("generationSteps[%d].output is empty", i)
		}
		// ordered reachability: вход не может ссылаться на output более позднего шага.
		for _, in := range st.Input {
			if allOutputs[in] && !produced[in] {
				return fmt.Errorf("generationSteps[%d] input %s is produced by a later step (ordered reachability)", i, in)
			}
		}
		for _, out := range st.Output {
			if seen[out] {
				return fmt.Errorf("duplicate generationStep output: %s", out)
			}
			seen[out] = true
			produced[out] = true
		}
	}
	for _, m := range yr.SupportedModes {
		if m != "prepare" && m != "auto" && m != "prepared" {
			return fmt.Errorf("unsupported mode: %s", m)
		}
	}
	return nil
}

// yamlRevision — структура YAML-файла конфигурации ревизии (Приложение A ТЗ).
type yamlRevision struct {
	Name                  string               `yaml:"name"`
	Version               string               `yaml:"version"`
	DisplayName           string               `yaml:"displayName"`
	Enabled               bool                 `yaml:"enabled"`
	RequiredInputFields   []string             `yaml:"requiredInputFields"`
	BaseInput             []string             `yaml:"baseInput"`
	GenerationSteps       []yamlGenerationStep `yaml:"generationSteps"`
	RevisionEffectiveDate string               `yaml:"revisionEffectiveDate"`
	SupportedModes        []string             `yaml:"supportedModes"`
	RenderAllowlist       []string             `yaml:"renderAllowlist"`
	RequiredRenderFields  []string             `yaml:"requiredRenderFields"`
	Defaults              map[string]string    `yaml:"defaults,omitempty"`
	Country               string               `yaml:"country,omitempty"`
	HeightCm              bool                 `yaml:"heightCm,omitempty"`
	Limits                *yamlLimits          `yaml:"limits,omitempty"`
	// Schema — секция схемы формы для фронтенда (п.14.5 ТЗ).
	// Если отсутствует в YAML — используется in-memory дефолт из NewMemoryStore.
	Schema *yamlSchema `yaml:"schema,omitempty"`
}

// yamlGenerationStep — grouped derive step в YAML (ПЛАН §3.1).
type yamlGenerationStep struct {
	ID       string   `yaml:"id"`
	Endpoint string   `yaml:"endpoint"`
	Input    []string `yaml:"input"`
	Output   []string `yaml:"output"`
}

// yamlLimits — лимиты профиля в YAML.
type yamlLimits struct {
	MaxUnits int `yaml:"maxUnits"`
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
	Name          string               `yaml:"name"`
	Type          string               `yaml:"type"`
	Required      bool                 `yaml:"required"`
	Label         string               `yaml:"label"`
	Order         int                  `yaml:"order"`
	Options       []string             `yaml:"options,omitempty"`
	OptionItems   []yamlOption         `yaml:"optionItems,omitempty"`
	FallbackValue string               `yaml:"fallbackValue,omitempty"`
	Validation    *yamlFieldValidation `yaml:"validation,omitempty"`
}

// yamlOption — typed {value,label} вариант closed-choice поля.
type yamlOption struct {
	Value string `yaml:"value"`
	Label string `yaml:"label"`
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

		if err := validateRevision(yr); err != nil {
			return fmt.Errorf("validate yaml %q: %w", path, err)
		}

		steps := make([]domain.GenerationStep, 0, len(yr.GenerationSteps))
		for _, st := range yr.GenerationSteps {
			steps = append(steps, domain.GenerationStep{
				ID:       st.ID,
				Endpoint: st.Endpoint,
				Input:    st.Input,
				Output:   st.Output,
			})
		}

		// required из fields[].required схемы, если явный список не задан (ПЛАН §3.1).
		required := yr.RequiredInputFields
		if len(required) == 0 && yr.Schema != nil {
			for _, f := range yr.Schema.Fields {
				if f.Required {
					required = append(required, f.Name)
				}
			}
		}

		var limits domain.Limits
		if yr.Limits != nil {
			limits = domain.Limits{MaxUnits: yr.Limits.MaxUnits}
		}

		// Схема — часть того же profile object (единое слияние, ПЛАН §3.1).
		var fields []domain.FieldSchema
		var groups []domain.FieldGroup
		if yr.Schema != nil {
			fields, groups = yamlSchemaToFields(yr.Schema)
		}

		s.configs[yr.Name] = domain.RevisionConfig{
			Name:                  yr.Name,
			Version:               yr.Version,
			DisplayName:           yr.DisplayName,
			Enabled:               yr.Enabled,
			RequiredInputFields:   required,
			BaseInput:             yr.BaseInput,
			GenerationSteps:       steps,
			RevisionEffectiveDate: yr.RevisionEffectiveDate,
			SupportedModes:        yr.SupportedModes,
			RenderAllowlist:       yr.RenderAllowlist,
			RequiredRenderFields:  yr.RequiredRenderFields,
			Defaults:              yr.Defaults,
			Country:               yr.Country,
			HeightCm:              yr.HeightCm,
			Limits:                limits,
			Fields:                fields,
			Groups:                groups,
		}
	}

	return nil
}

// yamlSchemaToFields конвертирует YAML-схему в поля/группы единого profile object.
func yamlSchemaToFields(ys *yamlSchema) ([]domain.FieldSchema, []domain.FieldGroup) {
	fields := make([]domain.FieldSchema, 0, len(ys.Fields))
	for _, f := range ys.Fields {
		fs := domain.FieldSchema{
			Name:          f.Name,
			Type:          f.Type,
			Required:      f.Required,
			Label:         f.Label,
			Order:         f.Order,
			Options:       f.Options,
			FallbackValue: f.FallbackValue,
		}
		for _, oi := range f.OptionItems {
			fs.OptionItems = append(fs.OptionItems, domain.FieldOption{Value: oi.Value, Label: oi.Label})
			// Значения optionItems также доступны как плоский Options (closed-choice).
			fs.Options = append(fs.Options, oi.Value)
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

	return fields, groups
}

// revisionSchemaToYAML конвертирует domain.RevisionSchema в YAML-структуру для персистентности.
func revisionSchemaToYAML(rs domain.RevisionSchema) *yamlSchema {
	if len(rs.Fields) == 0 {
		return nil
	}
	fields := make([]yamlFieldSchema, 0, len(rs.Fields))
	for _, f := range rs.Fields {
		yf := yamlFieldSchema{
			Name:          f.Name,
			Type:          f.Type,
			Required:      f.Required,
			Label:         f.Label,
			Order:         f.Order,
			Options:       f.Options,
			FallbackValue: f.FallbackValue,
		}
		for _, oi := range f.OptionItems {
			yf.OptionItems = append(yf.OptionItems, yamlOption{Value: oi.Value, Label: oi.Label})
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
