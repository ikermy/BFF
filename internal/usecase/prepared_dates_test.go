package usecase

import (
	"testing"

	"github.com/ikermy/BFF/internal/domain"
)

func TestValidatePreparedDates(t *testing.T) {
	t.Run("malformed → 422 INVALID_GENERATED_DATE", func(t *testing.T) {
		appErr := validatePreparedDates(map[string]any{"DBD": "00152024", "DBA": "00312030"}, "2022-03-07")
		if appErr == nil || appErr.HTTPStatus != 422 || appErr.Code != domain.ErrCodeInvalidGeneratedDate {
			t.Fatalf("got %+v", appErr)
		}
	})

	t.Run("before revision → 422 ISSUE_DATE_BEFORE_REVISION", func(t *testing.T) {
		appErr := validatePreparedDates(map[string]any{"DBD": "03062022", "DBA": "03062032"}, "2022-03-07")
		if appErr == nil || appErr.HTTPStatus != 422 || appErr.Code != domain.ErrCodeIssueDateBeforeRevision {
			t.Fatalf("got %+v", appErr)
		}
	})

	t.Run("valid → nil", func(t *testing.T) {
		if appErr := validatePreparedDates(map[string]any{"DBD": "03072022", "DBA": "03072032"}, "2022-03-07"); appErr != nil {
			t.Fatalf("got %+v", appErr)
		}
	})

	t.Run("no dates or no effectiveDate → nil (skip)", func(t *testing.T) {
		if appErr := validatePreparedDates(map[string]any{}, "2022-03-07"); appErr != nil {
			t.Fatalf("got %+v", appErr)
		}
		if appErr := validatePreparedDates(map[string]any{"DBD": "00152024", "DBA": "00312030"}, ""); appErr != nil {
			t.Fatalf("got %+v", appErr)
		}
	})
}
