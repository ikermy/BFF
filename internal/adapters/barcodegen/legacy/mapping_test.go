package legacy

import "testing"

func TestMapFields_HumanNamesToAAMVA(t *testing.T) {
	in := map[string]any{
		"firstName":    "John",
		"lastName":     "Doe",
		"street":       "123 Main St",
		"city":         "LA",
		"state":        "CA",
		"zipCode":      "90001",
		"dateOfBirth":  "08292017",
		"unknownField": "drop-me",
	}
	out := mapFields(in)
	checks := map[string]string{
		"DAC": "John", "DCS": "Doe", "DAG": "123 Main St", "DAI": "LA",
		"DAJ": "CA", "DAK": "90001", "DBB": "08292017",
	}
	for code, want := range checks {
		if got, ok := out[code]; !ok || got != want {
			t.Errorf("mapFields: expected %s=%v, got %v (present=%v)", code, want, got, ok)
		}
	}
	if _, ok := out["unknownField"]; ok {
		t.Error("mapFields: unknownField should be dropped (not in whitelist)")
	}
}

func TestMapFields_KeepsExistingAAMVA(t *testing.T) {
	in := map[string]any{"DAC": "John", "DCS": "Doe"}
	out := mapFields(in)
	if out["DAC"] != "John" || out["DCS"] != "Doe" {
		t.Errorf("mapFields: expected existing AAMVA codes preserved, got %v", out)
	}
}

func TestToAAMVA(t *testing.T) {
	if got := toAAMVA("firstName"); got != "DAC" {
		t.Errorf("toAAMVA(firstName)=%s, want DAC", got)
	}
	if got := toAAMVA("DAG"); got != "DAG" {
		t.Errorf("toAAMVA(DAG)=%s, want DAG", got)
	}
}

func TestLabelToAAMVA_CoversMapping(t *testing.T) {
	// Каждый AAMVA, на который ссылается прямая таблица, должен иметь обратный лейбл,
	// чтобы реверс-маппинг ответов random/calculate работал.
	if labelToAAMVA["Street:"] != "DAG" {
		t.Error("labelToAAMVA[Street:] != DAG")
	}
}
