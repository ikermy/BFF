package legacy

import "testing"

func TestParseRevision(t *testing.T) {
	pair, err := parseRevision("US_CA_08292017")
	if err != nil {
		t.Fatalf("parseRevision: %v", err)
	}
	if pair.State != "CA" || pair.Date != "08292017" {
		t.Errorf("parseRevision = %+v, want {CA 08292017}", pair)
	}
}

func TestParseRevision_Invalid(t *testing.T) {
	for _, rev := range []string{"", "CA_08292017", "US_CA", "US_CAX_08292017", "US_CA_0829"} {
		if _, err := parseRevision(rev); err == nil {
			t.Errorf("parseRevision(%q): expected error", rev)
		}
	}
}

func TestResolveRevision_Whitelist(t *testing.T) {
	SetSupportedRevisions([]struct{ State, Date string }{{State: "CA", Date: "08292017"}})
	defer SetSupportedRevisions(nil)

	if _, err := resolveRevision("US_CA_08292017"); err != nil {
		t.Fatalf("resolveRevision(US_CA_08292017): unexpected error %v", err)
	}
	if _, err := resolveRevision("US_TX_1234"); err == nil {
		t.Error("resolveRevision(US_TX_1234): expected whitelist error")
	}
}

func TestResolveRevision_EmptyWhitelistIsLenient(t *testing.T) {
	SetSupportedRevisions(nil)
	fields, err := resolveRevision("US_CA_08292017")
	if err != nil {
		t.Fatalf("resolveRevision with empty whitelist: %v", err)
	}
	if fields["DAJ"] != "CA" || fields["DDB"] != "08292017" {
		t.Errorf("resolveRevision fields = %v, want {DAJ:CA DDB:08292017}", fields)
	}
}

func TestSyncSupportedRevisionsFromConfigs(t *testing.T) {
	// Засев из имён конфигов: валидные пары попадают в whitelist, мусор отбрасывается.
	SyncSupportedRevisionsFromConfigs([]string{
		"US_CA_08292017",
		"US_NY_01202019",
		"not-a-revision",
	})
	defer SetSupportedRevisions(nil)

	if _, err := resolveRevision("US_CA_08292017"); err != nil {
		t.Errorf("US_CA_08292017 should be supported after sync: %v", err)
	}
	if _, err := resolveRevision("US_NY_01202019"); err != nil {
		t.Errorf("US_NY_01202019 should be supported after sync: %v", err)
	}
	// Пара, которой нет в конфигах BFF → не поддерживается.
	if _, err := resolveRevision("US_TX_06012020"); err == nil {
		t.Error("US_TX_06012020 should be rejected (not in seeded whitelist)")
	}
}
