package revisions

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromDir_ParsesGroupedProfile(t *testing.T) {
	dir := t.TempDir()
	doc := `name: US_TEST_01012020
displayName: Test Profile
enabled: true
revisionEffectiveDate: "2020-01-01"
supportedModes: [prepare, prepared]
renderAllowlist: [DAJ, DDB, QQQ]
generationSteps:
  - id: date
    endpoint: random
    input: [DAJ, DBB]
    output: [DBD, DBA]
  - id: inventory
    endpoint: calculate
    input: [DAJ, DBD, DAQ]
    output: [DCK]
`
	if err := os.WriteFile(filepath.Join(dir, "US_TEST_01012020.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewMemoryStore()
	if err := s.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	cfg, err := s.GetConfig(context.Background(), "US_TEST_01012020")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if cfg.RevisionEffectiveDate != "2020-01-01" {
		t.Fatalf("effectiveDate = %q", cfg.RevisionEffectiveDate)
	}
	if len(cfg.SupportedModes) != 2 || cfg.SupportedModes[0] != "prepare" {
		t.Fatalf("supportedModes = %v", cfg.SupportedModes)
	}
	if len(cfg.RenderAllowlist) != 3 {
		t.Fatalf("renderAllowlist = %v", cfg.RenderAllowlist)
	}
	if len(cfg.GenerationSteps) != 2 {
		t.Fatalf("generationSteps = %d, want 2", len(cfg.GenerationSteps))
	}
	if cfg.GenerationSteps[0].Endpoint != "random" || cfg.GenerationSteps[0].Output[0] != "DBD" {
		t.Fatalf("step0 = %+v", cfg.GenerationSteps[0])
	}
}

func TestLoadFromDir_RejectsInvalidEffectiveDate(t *testing.T) {
	dir := t.TempDir()
	doc := "name: US_BAD\ndisplayName: Bad\nrevisionEffectiveDate: 01/01/2020\n"
	if err := os.WriteFile(filepath.Join(dir, "US_BAD.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewMemoryStore()
	if err := s.LoadFromDir(dir); err == nil {
		t.Fatal("expected error for non-ISO revisionEffectiveDate")
	}
}

func TestLoadFromDir_DerivesRequiredAndLimits(t *testing.T) {
	dir := t.TempDir()
	doc := `name: US_REQ
displayName: Req
limits:
  maxUnits: 5
schema:
  fields:
    - name: firstName
      type: string
      required: true
      label: First
      order: 1
    - name: city
      type: string
      required: false
      label: City
      order: 2
`
	if err := os.WriteFile(filepath.Join(dir, "US_REQ.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewMemoryStore()
	if err := s.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	cfg, err := s.GetConfig(context.Background(), "US_REQ")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.RequiredInputFields) != 1 || cfg.RequiredInputFields[0] != "firstName" {
		t.Fatalf("required = %v", cfg.RequiredInputFields)
	}
	if cfg.Limits.MaxUnits != 5 {
		t.Fatalf("limits = %+v", cfg.Limits)
	}
}

func TestLoadFromDir_TypedOptionItems(t *testing.T) {
	dir := t.TempDir()
	doc := `name: US_OPT
displayName: Opt
schema:
  fields:
    - name: eyeColor
      type: enum
      required: true
      label: Eyes
      order: 1
      optionItems:
        - {value: BLK, label: Black}
        - {value: BRO, label: Brown}
`
	if err := os.WriteFile(filepath.Join(dir, "US_OPT.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewMemoryStore()
	if err := s.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	schema, err := s.GetSchema(context.Background(), "US_OPT")
	if err != nil {
		t.Fatal(err)
	}
	f := schema.Fields[0]
	if len(f.OptionItems) != 2 || f.OptionItems[1].Label != "Brown" {
		t.Fatalf("optionItems = %+v", f.OptionItems)
	}
	if len(f.Options) != 2 || f.Options[0] != "BLK" {
		t.Fatalf("options = %v", f.Options)
	}
}

func TestLoadFromDir_RejectsForwardReference(t *testing.T) {
	dir := t.TempDir()
	doc := `name: US_FWD
displayName: fwd
generationSteps:
  - id: a
    endpoint: calculate
    input: [DCK]
    output: [DCF]
  - id: b
    endpoint: calculate
    input: [DAJ]
    output: [DCK]
`
	if err := os.WriteFile(filepath.Join(dir, "US_FWD.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewMemoryStore()
	if err := s.LoadFromDir(dir); err == nil {
		t.Fatal("expected ordered-reachability error for forward reference")
	}
}

func TestLoadFromDir_RejectsDuplicateStepOutput(t *testing.T) {
	dir := t.TempDir()
	doc := `name: US_DUP
displayName: Dup
generationSteps:
  - id: a
    endpoint: random
    input: [DAJ]
    output: [DBD]
  - id: b
    endpoint: calculate
    input: [DAJ]
    output: [DBD]
`
	if err := os.WriteFile(filepath.Join(dir, "US_DUP.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewMemoryStore()
	if err := s.LoadFromDir(dir); err == nil {
		t.Fatal("expected error for duplicate output")
	}
}
