package revisions

import (
	"context"
	"testing"
)

// TestRegistry_LoadsAllProfiles — release-gate harness: все профили каталога
// configs/revisions должны валидно загружаться (fail-fast при ошибке любого).
func TestRegistry_LoadsAllProfiles(t *testing.T) {
	s := NewMemoryStore()
	if err := s.LoadFromDir("../../../configs/revisions"); err != nil {
		t.Fatalf("LoadFromDir(configs/revisions): %v", err)
	}

	expected := []string{
		"US_CA_08292017",
		"US_WA_11122019",
		"US_CO_10302015",
		"US_LA_02102015",
		"US_OH_07012018",
		"US_MI_Rev_01-21-2011",
	}
	for _, name := range expected {
		cfg, err := s.GetConfig(context.Background(), name)
		if err != nil {
			t.Fatalf("profile %s not loaded: %v", name, err)
		}
		if name != "US_CA_08292017" && cfg.RevisionEffectiveDate == "" {
			t.Errorf("profile %s: revisionEffectiveDate is empty", name)
		}
	}

	// LA: auditCode (DCJ) НЕ генерируется — ни один шаг не выпускает DCJ.
	la, _ := s.GetConfig(context.Background(), "US_LA_02102015")
	for _, step := range la.GenerationSteps {
		for _, out := range step.Output {
			if out == "DCJ" {
				t.Errorf("LA must not generate DCJ, got step %s", step.ID)
			}
		}
	}
	// WA: DCJ генерируется.
	wa, _ := s.GetConfig(context.Background(), "US_WA_11122019")
	hasDCJ := false
	for _, step := range wa.GenerationSteps {
		for _, out := range step.Output {
			if out == "DCJ" {
				hasDCJ = true
			}
		}
	}
	if !hasDCJ {
		t.Error("WA must declare audit (DCJ) generation step")
	}
}

// TestRegistry_ProfileCount — полный реестр профилей: 55 (54 source configs,
// California DL/ID split). ПЛАН/матрица BFF_ПОЛЯ_И_ЦЕПОЧКИ_ПО_РЕВИЗИЯМ.
func TestRegistry_ProfileCount(t *testing.T) {
	s := NewMemoryStore()
	if err := s.LoadFromDir("../../../configs/revisions"); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	list, err := s.ListConfigs(context.Background())
	if err != nil {
		t.Fatalf("ListConfigs: %v", err)
	}
	if len(list) != 55 {
		t.Fatalf("expected 55 profiles, got %d", len(list))
	}
	// Уникальность profile ID (имён).
	seen := map[string]bool{}
	for _, cfg := range list {
		if seen[cfg.Name] {
			t.Fatalf("duplicate profile id: %s", cfg.Name)
		}
		seen[cfg.Name] = true
	}
}

// TestRegistry_SchemaFromSameProfileObject — ПЛАН §3.1: schema и config берутся
// из одного загруженного profile object, отдельного schema-store нет.
func TestRegistry_SchemaFromSameProfileObject(t *testing.T) {
	s := NewMemoryStore()
	if err := s.LoadFromDir("../../../configs/revisions"); err != nil {
		t.Fatalf("LoadFromDir(configs/revisions): %v", err)
	}

	cfg, err := s.GetConfig(context.Background(), "US_CA_08292017")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if len(cfg.Fields) == 0 || len(cfg.Groups) == 0 {
		t.Fatal("schema fields/groups must be part of the config object")
	}

	schema, err := s.GetSchema(context.Background(), "US_CA_08292017")
	if err != nil {
		t.Fatalf("GetSchema: %v", err)
	}
	if schema.Revision != cfg.Name || schema.DisplayName != cfg.DisplayName {
		t.Fatalf("schema identity mismatch: %+v vs cfg %+v", schema, cfg)
	}
	if schema.RevisionEffectiveDate != cfg.RevisionEffectiveDate {
		t.Fatalf("effectiveDate mismatch: %q vs %q", schema.RevisionEffectiveDate, cfg.RevisionEffectiveDate)
	}
	if len(schema.Fields) != len(cfg.Fields) || len(schema.Groups) != len(cfg.Groups) {
		t.Fatalf("schema must be derived from the same object: fields %d/%d groups %d/%d",
			len(schema.Fields), len(cfg.Fields), len(schema.Groups), len(cfg.Groups))
	}
	if len(schema.GeneratedFields) == 0 {
		t.Fatal("generatedFields must be derived from generationSteps")
	}
	hasDOB := false
	for _, f := range schema.BaseInput {
		if f == "dateOfBirth" {
			hasDOB = true
		}
	}
	if !hasDOB {
		t.Fatalf("baseInput must include dateOfBirth, got %v", schema.BaseInput)
	}
}

// TestRegistry_BaseInputExcludesGeneratedRequired — baseInput (engine-mapped база)
// не включает генерируемые обязательные поля вроде auditCode; при отсутствии
// baseInput в YAML он дефолтится в requiredInputFields.
func TestRegistry_BaseInputExcludesGeneratedRequired(t *testing.T) {
	s := NewMemoryStore()
	if err := s.LoadFromDir("../../../configs/revisions"); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	la, err := s.GetSchema(context.Background(), "US_LA_02102015")
	if err != nil {
		t.Fatalf("LA GetSchema: %v", err)
	}
	hasAudit, hasDOB := false, false
	for _, f := range la.BaseInput {
		if f == "auditCode" {
			hasAudit = true
		}
		if f == "dateOfBirth" {
			hasDOB = true
		}
	}
	if hasAudit {
		t.Fatalf("LA baseInput must NOT include generated-required auditCode, got %v", la.BaseInput)
	}
	if !hasDOB {
		t.Fatalf("LA baseInput must include dateOfBirth, got %v", la.BaseInput)
	}

	// WA не объявляет baseInput → дефолт на requiredInputFields (без auditCode).
	wa, err := s.GetSchema(context.Background(), "US_WA_11122019")
	if err != nil {
		t.Fatalf("WA GetSchema: %v", err)
	}
	if len(wa.BaseInput) == 0 {
		t.Fatal("WA baseInput must default to requiredInputFields")
	}
}
