package usecase

import (
	"testing"
	"time"
)

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

func mustKind(t *testing.T, err error, want DateErrorKind) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error kind %v, got nil", want)
	}
	de, ok := err.(*DateValidationError)
	if !ok {
		t.Fatalf("expected *DateValidationError, got %T: %v", err, err)
	}
	if de.Kind != want {
		t.Fatalf("kind = %v, want %v (%v)", de.Kind, want, err)
	}
}

func TestValidateGeneratedDates_Acceptance(t *testing.T) {
	// фиксируем "сегодня" так, чтобы 2022/2024 были в прошлом
	clk := fixedClock{t: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)}

	t.Run("DBD=00152024 отклоняется (month 00)", func(t *testing.T) {
		mustKind(t, ValidateGeneratedDates("00152024", "00312030", "2022-03-07", clk), DateMalformed)
	})

	t.Run("DBA=00312030 отклоняется (month 00)", func(t *testing.T) {
		mustKind(t, ValidateGeneratedDates("03072022", "00312030", "2022-03-07", clk), DateMalformed)
	})

	t.Run("DBD=03062022 раньше ревизии 2022-03-07", func(t *testing.T) {
		mustKind(t, ValidateGeneratedDates("03062022", "03062032", "2022-03-07", clk), DateBeforeRevision)
	})

	t.Run("DBD=03072022 принимается", func(t *testing.T) {
		if err := ValidateGeneratedDates("03072022", "03072032", "2022-03-07", clk); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("невалидный календарь 02312024", func(t *testing.T) {
		mustKind(t, ValidateGeneratedDates("02312024", "02312034", "2022-03-07", clk), DateMalformed)
	})

	t.Run("месяц 13", func(t *testing.T) {
		mustKind(t, ValidateGeneratedDates("13012024", "13012034", "2022-03-07", clk), DateMalformed)
	})

	t.Run("неверная длина / нецифры", func(t *testing.T) {
		mustKind(t, ValidateGeneratedDates("0307202", "03072032", "2022-03-07", clk), DateMalformed)
		mustKind(t, ValidateGeneratedDates("03O72022", "03072032", "2022-03-07", clk), DateMalformed)
	})

	t.Run("future issue date отклоняется", func(t *testing.T) {
		mustKind(t, ValidateGeneratedDates("01012030", "01012040", "2022-03-07", clk), DateFuture)
	})

	t.Run("expiration не позже issue", func(t *testing.T) {
		mustKind(t, ValidateGeneratedDates("03072022", "03072022", "2022-03-07", clk), ExpirationNotAfter)
	})
}

func TestFetchValidatedDateStep_Retries(t *testing.T) {
	clk := fixedClock{t: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)}
	calls := 0
	fetch := func() (string, string, error) {
		calls++
		switch calls {
		case 1:
			return "00152024", "00312030", nil // malformed
		case 2:
			return "03062022", "03062032", nil // before revision
		default:
			return "03072022", "03072032", nil // valid
		}
	}
	dbd, dba, err := FetchValidatedDateStep(20, fetch, "2022-03-07", clk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dbd != "03072022" || dba != "03072032" {
		t.Fatalf("got %s/%s", dbd, dba)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestFetchValidatedDateStep_Exhausts(t *testing.T) {
	clk := fixedClock{t: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)}
	calls := 0
	fetch := func() (string, string, error) {
		calls++
		return "00152024", "00312030", nil
	}
	_, _, err := FetchValidatedDateStep(5, fetch, "2022-03-07", clk)
	mustKind(t, err, DateMalformed)
	if calls != 5 {
		t.Fatalf("calls = %d, want 5", calls)
	}
}
