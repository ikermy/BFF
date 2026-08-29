package revisions

import (
	"context"
	"path/filepath"
	"testing"
)

// TestRevisionsYAML_CAChainCompatibleWithBarcodeGen — регрессия B4 (отчёт §4.2 п.5).
//
// Правка данных, не кода: calculationChain ревизий ДОЛЖЕН быть выполним на реальном
// BarcodeGen, иначе chain-исполнение падает с FIELD_SOURCE_UNSUPPORTED.
// calculate поддерживает только [DBA],[DCK],[DCF]; random — только 6 фиксированных
// наборов ([DAQ], [DBB], [DAG,DAI,DAK], [DAC,DAD,DCS], [DBD,DBA], [DCJ]).
// Проверяем реальный YAML-файл из каталога configs/revisions.
func TestRevisionsYAML_CAChainCompatibleWithBarcodeGen(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "configs", "revisions")
	store := NewMemoryStore()
	if err := store.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir(%q): %v", dir, err)
	}

	cfg, err := store.GetConfig(context.Background(), "US_CA_08292017")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if len(cfg.CalculationChain) == 0 {
		t.Fatal("expected non-empty calculationChain loaded from YAML")
	}

	for _, step := range cfg.CalculationChain {
		switch step.Source {
		case "calculate":
			switch step.Field {
			case "DBA", "DCK", "DCF":
			default:
				t.Errorf("chain step %s: BarcodeGen calculate supports only [DBA,DCK,DCF], got %s", step.Field, step.Field)
			}
		case "random":
			if !randomFieldSupported(step.Field) {
				t.Errorf("chain step %s: not in any BarcodeGen random set", step.Field)
			}
		case "user":
			// source=user — поле заполняет пользователь, BarcodeGen не вызывается. Ок.
		default:
			t.Errorf("chain step %s: unexpected source %q", step.Field, step.Source)
		}
	}
}

func randomFieldSupported(field string) bool {
	switch field {
	case "DAQ", "DBB", "DAG", "DAI", "DAK", "DAC", "DAD", "DCS", "DBD", "DBA", "DCJ":
		return true
	}
	return false
}
