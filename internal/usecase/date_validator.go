package usecase

import (
	"fmt"
	"time"
)

// Clock — инъецируемые UTC-часы (для детерминированных тестов и проверки future date).
type Clock interface {
	Now() time.Time
}

// RealClock — часы по умолчанию.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

// DateErrorKind — категория ошибки валидатора дат (маппится в HTTP-код на слое transport).
type DateErrorKind int

const (
	// DateMalformed — DBD/DBA не 8 цифр, месяц 00/вне 01..12, несуществующая календарная дата.
	DateMalformed DateErrorKind = iota
	// DateBeforeRevision — issueDate раньше revisionEffectiveDate.
	DateBeforeRevision
	// DateFuture — issueDate позже today.
	DateFuture
	// ExpirationNotAfter — expirationDate <= issueDate.
	ExpirationNotAfter
)

// DateValidationError — типизированная ошибка валидации пары [DBD, DBA].
type DateValidationError struct {
	Kind    DateErrorKind
	Message string
}

func (e *DateValidationError) Error() string { return e.Message }

// ValidateGeneratedDates проверяет атомарный ответ date-step [DBD, DBA]
// (МИКРО_ТЗ_ВАЛИДАТОР_СГЕНЕРИРОВАННЫХ_ДАТ.md). Даты в формате MMDDYYYY.
// revisionEffectiveDate — ISO YYYY-MM-DD из статического профиля.
//
// Проверки: 8 цифр, месяц 01..12 (00 запрещён), календарная валидность
// (round-trip), issueDate >= revisionEffectiveDate, issueDate <= today,
// expirationDate > issueDate. Возвращает первый нарушенный инвариант.
func ValidateGeneratedDates(dbd, dba, revisionEffectiveDate string, clk Clock) error {
	issue, err := parseMMDDYYYY(dbd)
	if err != nil {
		return &DateValidationError{Kind: DateMalformed, Message: fmt.Sprintf("invalid DBD: %s", err.Error())}
	}
	exp, err := parseMMDDYYYY(dba)
	if err != nil {
		return &DateValidationError{Kind: DateMalformed, Message: fmt.Sprintf("invalid DBA: %s", err.Error())}
	}

	eff, err := parseISODate(revisionEffectiveDate)
	if err != nil {
		return &DateValidationError{Kind: DateMalformed, Message: fmt.Sprintf("invalid revisionEffectiveDate: %s", err.Error())}
	}

	if issue.Before(eff) {
		return &DateValidationError{Kind: DateBeforeRevision, Message: "ISSUE_DATE_BEFORE_REVISION"}
	}

	today := dateOnly(clk.Now())
	if issue.After(today) {
		return &DateValidationError{Kind: DateFuture, Message: "ISSUE_DATE_IN_FUTURE"}
	}

	if !exp.After(issue) {
		return &DateValidationError{Kind: ExpirationNotAfter, Message: "EXPIRATION_NOT_AFTER_ISSUE"}
	}

	return nil
}

// DateStepFetcher возвращает очередную пару [DBD, DBA] от date-step BarcodeGen.
type DateStepFetcher func() (dbd, dba string, err error)

// FetchValidatedDateStep повторяет date-step до attempts раз, пока пара не пройдёт
// ValidateGeneratedDates (prepare/auto). Сетевые ошибки и невалидные пары повторяются
// одной и той же ручкой; при исчерпании лимита возвращается последняя ошибка
// (caller маппит её в 502 BARCODEGEN_INVALID_ISSUE_DATE).
func FetchValidatedDateStep(attempts int, fetch DateStepFetcher, revisionEffectiveDate string, clk Clock) (string, string, error) {
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		dbd, dba, err := fetch()
		if err != nil {
			lastErr = err
			continue
		}
		if verr := ValidateGeneratedDates(dbd, dba, revisionEffectiveDate, clk); verr != nil {
			lastErr = verr
			continue
		}
		return dbd, dba, nil
	}
	if lastErr == nil {
		lastErr = &DateValidationError{Kind: DateMalformed, Message: "DATE_STEP_ATTEMPTS_EXHAUSTED"}
	}
	return "", "", lastErr
}

// parseMMDDYYYY строго парсит MMDDYYYY: ровно 8 ASCII-цифр, месяц 01..12,
// дата существует в календаре (round-trip).
func parseMMDDYYYY(s string) (time.Time, error) {
	if len(s) != 8 {
		return time.Time{}, fmt.Errorf("expected 8 digits, got %d", len(s))
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return time.Time{}, fmt.Errorf("non-digit character")
		}
	}
	month := int(s[0]-'0')*10 + int(s[1]-'0')
	day := int(s[2]-'0')*10 + int(s[3]-'0')
	year := 0
	for i := 4; i < 8; i++ {
		year = year*10 + int(s[i]-'0')
	}
	if month < 1 || month > 12 {
		return time.Time{}, fmt.Errorf("month %02d out of 01..12", month)
	}
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if t.Month() != time.Month(month) || t.Day() != day || t.Year() != year {
		return time.Time{}, fmt.Errorf("date does not exist in calendar")
	}
	return t, nil
}

func parseISODate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, err
	}
	return dateOnly(t), nil
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
