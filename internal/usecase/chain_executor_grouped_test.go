package usecase

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikermy/BFF/internal/adapters/barcodegen"
	"github.com/ikermy/BFF/internal/adapters/revisions"
	"github.com/ikermy/BFF/internal/domain"
)

// scriptedDeriver возвращает значения по output; при dateStep попытки могут
// возвращать невалидную пару из очереди retries, затем валидную.
type scriptedDeriver struct {
	calls   []string
	retries [][2]string // очередь [DBD,DBA] для первых вызовов date-step
	attempt int
}

func (d *scriptedDeriver) Derive(_ context.Context, _, endpoint string, _ map[string]any, output []string) (map[string]any, error) {
	d.calls = append(d.calls, endpoint+":"+strings.Join(output, ","))
	out := make(map[string]any, len(output))
	for _, o := range output {
		switch o {
		case "DBD":
			if d.attempt < len(d.retries) {
				out[o] = d.retries[d.attempt][0]
			} else {
				out[o] = "03072022"
			}
		case "DBA":
			if d.attempt < len(d.retries) {
				out[o] = d.retries[d.attempt][1]
			} else {
				out[o] = "03072032"
			}
		case "DCK":
			out[o] = "INV-1"
		default:
			out[o] = "V-" + o
		}
	}
	if output[0] == "DBD" {
		d.attempt++
	}
	return out, nil
}

func groupedStore(t *testing.T) *revisions.MemoryStore {
	t.Helper()
	dir := t.TempDir()
	doc := `name: US_GROUP_01012020
displayName: Grouped Test
enabled: true
revisionEffectiveDate: "2022-03-07"
supportedModes: [prepare, auto, prepared]
generationSteps:
  - id: date
    endpoint: random
    input: [DAJ, DBB]
    output: [DBD, DBA]
  - id: inv
    endpoint: calculate
    input: [DAJ, DBD, DAQ]
    output: [DCK]
`
	if err := os.WriteFile(filepath.Join(dir, "US_GROUP_01012020.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	store := revisions.NewMemoryStore()
	if err := store.LoadFromDir(dir); err != nil {
		t.Fatal(err)
	}
	return store
}

// containsStr — вспомогательная функция для поиска строки в срезе.
func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func TestExecuteGrouped_InvalidRevision(t *testing.T) {
	store := groupedStore(t)
	exec := NewChainExecutor(barcodegen.NewMockClient(), store)

	if _, err := exec.ExecuteGrouped(context.Background(), "NON_EXISTENT_REVISION", map[string]any{}); err == nil {
		t.Fatal("expected error for unknown revision")
	}
}

func TestExecuteGrouped_ComputesSteps(t *testing.T) {
	store := groupedStore(t)
	clk := fixedClock{t: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)}
	d := &scriptedDeriver{}
	exec := NewChainExecutor(barcodegen.NewMockClient(), store).WithDeriver(d).WithClock(clk)

	res, err := exec.ExecuteGrouped(context.Background(), "US_GROUP_01012020", map[string]any{"DAJ": "AR", "DBB": "07191994"})
	if err != nil {
		t.Fatalf("ExecuteGrouped: %v", err)
	}
	if res.Fields["DBD"] != "03072022" || res.Fields["DBA"] != "03072032" || res.Fields["DCK"] != "INV-1" {
		t.Fatalf("fields = %v", res.Fields)
	}
	for _, f := range []string{"DBD", "DBA", "DCK"} {
		if !containsStr(res.Computed, f) {
			t.Fatalf("%s not in Computed: %v", f, res.Computed)
		}
	}
}

func TestExecuteGrouped_DoesNotOverwriteUserValue(t *testing.T) {
	store := groupedStore(t)
	clk := fixedClock{t: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)}
	exec := NewChainExecutor(barcodegen.NewMockClient(), store).WithDeriver(&scriptedDeriver{}).WithClock(clk)

	res, err := exec.ExecuteGrouped(context.Background(), "US_GROUP_01012020", map[string]any{
		"DAJ": "AR", "DBB": "07191994", "DCK": "USER-DCK",
	})
	if err != nil {
		t.Fatalf("ExecuteGrouped: %v", err)
	}
	if res.Fields["DCK"] != "USER-DCK" {
		t.Fatalf("DCK overwritten: %v", res.Fields["DCK"])
	}
	if !containsStr(res.Skipped, "DCK") || containsStr(res.Computed, "DCK") {
		t.Fatalf("DCK skip/computed wrong: skipped=%v computed=%v", res.Skipped, res.Computed)
	}
}

func TestExecuteGrouped_DateAttemptsExhausted(t *testing.T) {
	store := groupedStore(t)
	clk := fixedClock{t: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)}
	// Все попытки дают month 00 → после лимита 502 BARCODEGEN_INVALID_ISSUE_DATE.
	d := &scriptedDeriver{retries: [][2]string{{"00152024", "00312030"}}}
	d.retries = append(d.retries,
		[2]string{"00152024", "00312030"},
		[2]string{"00152024", "00312030"},
	)
	exec := NewChainExecutor(barcodegen.NewMockClient(), store).WithDeriver(d).WithClock(clk).WithDateAttempts(3)

	_, err := exec.ExecuteGrouped(context.Background(), "US_GROUP_01012020", map[string]any{"DAJ": "AR", "DBB": "07191994"})
	if err == nil {
		t.Fatal("expected error after date attempts exhausted")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected *domain.AppError, got %T: %v", err, err)
	}
	if appErr.Code != domain.ErrCodeBarcodeGenInvalidDate || appErr.HTTPStatus != 502 {
		t.Fatalf("got code=%s status=%d", appErr.Code, appErr.HTTPStatus)
	}
}

func TestExecuteGrouped_DateStepRetries(t *testing.T) {
	store := groupedStore(t)
	clk := fixedClock{t: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)}
	d := &scriptedDeriver{retries: [][2]string{
		{"00152024", "00312030"}, // malformed
		{"03062022", "03062032"}, // before revision
	}}
	exec := NewChainExecutor(barcodegen.NewMockClient(), store).WithDeriver(d).WithClock(clk).WithDateAttempts(20)

	res, err := exec.ExecuteGrouped(context.Background(), "US_GROUP_01012020", map[string]any{"DAJ": "AR", "DBB": "07191994"})
	if err != nil {
		t.Fatalf("ExecuteGrouped: %v", err)
	}
	if res.Fields["DBD"] != "03072022" || res.Fields["DBA"] != "03072032" {
		t.Fatalf("fields = %v", res.Fields)
	}
	if d.attempt != 3 {
		t.Fatalf("date-step attempts = %d, want 3", d.attempt)
	}
}
