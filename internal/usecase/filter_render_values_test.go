package usecase

import "testing"

func TestFilterRenderValues(t *testing.T) {
	fields := map[string]any{"DAJ": "AR", "DDB": "03012018", "QQQ": "DL", "firstName": "X", "signatureUrl": "http://sig"}

	t.Run("empty allowlist is permissive", func(t *testing.T) {
		got := filterRenderValues(nil, fields)
		if len(got) != len(fields) {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("allowlist filters non-engine fields", func(t *testing.T) {
		got := filterRenderValues([]string{"DAJ", "DDB", "QQQ"}, fields)
		if len(got) != 3 {
			t.Fatalf("got %v", got)
		}
		if _, ok := got["firstName"]; ok {
			t.Fatal("firstName must be filtered out")
		}
		if _, ok := got["signatureUrl"]; ok {
			t.Fatal("signatureUrl must be filtered out")
		}
	})
}
