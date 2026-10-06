package revisions

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
)

// TestRegistry_GoldenFixtures — контрактный gate профилей: загруженные YAML
// должны точно соответствовать фикстуре, сгенерированной из авторитетной матрицы
// (tools/gen_profiles.mjs --fixtures). Фикстура покрывает 49 сгенерированных
// профилей; CA/WA/CO/LA/OH/MI — hand-tuned и покрыты отдельными тестами.
type fxStep struct {
	ID       string   `json:"id"`
	Endpoint string   `json:"endpoint"`
	Input    []string `json:"input"`
	Output   []string `json:"output"`
}

type fxProfile struct {
	Name                  string   `json:"name"`
	Country               string   `json:"country"`
	HeightCm              bool     `json:"heightCm"`
	RevisionEffectiveDate string   `json:"revisionEffectiveDate"`
	SupportedModes        []string `json:"supportedModes"`
	RequiredInputFields   []string `json:"requiredInputFields"`
	RenderAllowlist       []string `json:"renderAllowlist"`
	Steps                 []fxStep `json:"steps"`
}

type fxFile struct {
	Profiles []fxProfile `json:"profiles"`
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func TestRegistry_GoldenFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/profiles.json")
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var fx fxFile
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	if len(fx.Profiles) != 49 {
		t.Fatalf("expected 49 fixtures, got %d", len(fx.Profiles))
	}

	store := NewMemoryStore()
	if err := store.LoadFromDir("../../../configs/revisions"); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	for _, f := range fx.Profiles {
		cfg, err := store.GetConfig(context.Background(), f.Name)
		if err != nil {
			t.Errorf("%s: GetConfig: %v", f.Name, err)
			continue
		}
		if cfg.Country != f.Country {
			t.Errorf("%s: country = %q, want %q", f.Name, cfg.Country, f.Country)
		}
		if cfg.HeightCm != f.HeightCm {
			t.Errorf("%s: heightCm = %v, want %v", f.Name, cfg.HeightCm, f.HeightCm)
		}
		if cfg.RevisionEffectiveDate != f.RevisionEffectiveDate {
			t.Errorf("%s: effectiveDate = %q, want %q", f.Name, cfg.RevisionEffectiveDate, f.RevisionEffectiveDate)
		}
		if !reflect.DeepEqual(sortedCopy(cfg.SupportedModes), sortedCopy(f.SupportedModes)) {
			t.Errorf("%s: modes = %v, want %v", f.Name, cfg.SupportedModes, f.SupportedModes)
		}
		if !reflect.DeepEqual(sortedCopy(cfg.RequiredInputFields), sortedCopy(f.RequiredInputFields)) {
			t.Errorf("%s: required = %v, want %v", f.Name, cfg.RequiredInputFields, f.RequiredInputFields)
		}
		if !reflect.DeepEqual(sortedCopy(cfg.RenderAllowlist), sortedCopy(f.RenderAllowlist)) {
			t.Errorf("%s: allowlist = %v, want %v", f.Name, cfg.RenderAllowlist, f.RenderAllowlist)
		}
		if len(cfg.GenerationSteps) != len(f.Steps) {
			t.Errorf("%s: steps = %d, want %d", f.Name, len(cfg.GenerationSteps), len(f.Steps))
			continue
		}
		for i, want := range f.Steps {
			got := cfg.GenerationSteps[i]
			if got.ID != want.ID || got.Endpoint != want.Endpoint ||
				!reflect.DeepEqual(got.Input, want.Input) || !reflect.DeepEqual(got.Output, want.Output) {
				t.Errorf("%s step %d: got %+v, want %+v", f.Name, i, got, want)
			}
		}
	}
}
