package revisions

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestRevisionsYAML_CAGenerationStepsCompatibleWithBarcodeGen — регрессия B4.
//
// Правка данных, не кода: generationSteps ревизий ДОЛЖНЫ быть выполнимы на
// реальном BarcodeGen, иначе chain-исполнение падает с FIELD_SOURCE_UNSUPPORTED.
// calculate поддерживает только [DBA],[DCK],[DCF]; random — только фиксированные
// наборы ([DAQ], [DBB], [DAG,DAI,DAK], [DAC,DAD,DCS], [DBD,DBA], [DCJ]).
// Проверяем реальный YAML-файл из каталога configs/revisions.
func TestRevisionsYAML_CAGenerationStepsCompatibleWithBarcodeGen(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "configs", "revisions")
	store := NewMemoryStore()
	if err := store.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir(%q): %v", dir, err)
	}

	cfg, err := store.GetConfig(context.Background(), "US_CA_08292017")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if len(cfg.GenerationSteps) == 0 {
		t.Fatal("expected non-empty generationSteps loaded from YAML")
	}

	for _, step := range cfg.GenerationSteps {
		switch step.Endpoint {
		case "calculate":
			// calculate умеет только одиночные поля [DBA],[DCK],[DCF].
			if len(step.Output) != 1 {
				t.Errorf("step %s: calculate supports single-field output, got %v", step.ID, step.Output)
				continue
			}
			switch step.Output[0] {
			case "DBA", "DCK", "DCF":
			default:
				t.Errorf("step %s: BarcodeGen calculate supports only [DBA,DCK,DCF], got %s", step.ID, step.Output[0])
			}
		case "random":
			if !randomSetSupported(step.Output) {
				t.Errorf("step %s: output %v is not a known BarcodeGen random set", step.ID, step.Output)
			}
		default:
			t.Errorf("step %s: unexpected endpoint %q", step.ID, step.Endpoint)
		}
	}
}

// randomSetSupported — output шага должен совпадать с одним из фиксированных
// random-наборов BarcodeGen.
func randomSetSupported(output []string) bool {
	sets := [][]string{
		{"DAQ"},
		{"DBB"},
		{"DAG", "DAI", "DAK"},
		{"DAC", "DAD", "DCS"},
		{"DBD", "DBA"},
		{"DCJ"},
	}
	want := append([]string(nil), output...)
	sort.Strings(want)
	for _, set := range sets {
		got := append([]string(nil), set...)
		sort.Strings(got)
		if strings.Join(want, ",") == strings.Join(got, ",") {
			return true
		}
	}
	return false
}
