package domain

import "testing"

func TestStableGenerationID_DeterministicAndUnique(t *testing.T) {
	a := StableGenerationID("idem-1", 0)
	b := StableGenerationID("idem-1", 0)
	if a != b {
		t.Fatalf("same input must yield same id: %q vs %q", a, b)
	}
	if StableGenerationID("idem-1", 1) == a {
		t.Fatal("different index must yield different id")
	}
	if StableGenerationID("idem-2", 0) == a {
		t.Fatal("different key must yield different id")
	}
	if len(a) != 36 {
		t.Fatalf("expected UUID-like length 36, got %d (%q)", len(a), a)
	}
}
