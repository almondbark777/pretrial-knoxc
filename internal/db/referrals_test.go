package db

import "testing"

// ReferralEntries lists app-entered referrals newest-first and drops a
// supervisor-tombstoned one — the same suppression every view honors.
func TestReferralEntriesListAndTombstone(t *testing.T) {
	d := openEnsured(t)
	older, newer := "999888001", "999888002"
	mustExec := func(idn, name, created string) {
		t.Helper()
		if _, err := d.Exec(
			`INSERT INTO added_defendants (idn, defendant, warrant_case_num, supervising_officer, referral_date, author, created_at) VALUES (?,?,?,?,?,?,?)`,
			idn, name, "@"+idn, "jane.doe@knoxsheriff.org", "2026-06-01", "officer@knoxsheriff.org", created); err != nil {
			t.Fatalf("insert referral: %v", err)
		}
	}
	mustExec(older, "ZZREF, OLDER", "2026-06-01 09:00:00")
	mustExec(newer, "ZZREF, NEWER", "2026-06-02 09:00:00")

	entries, err := ReferralEntries(d)
	if err != nil {
		t.Fatalf("ReferralEntries: %v", err)
	}
	// Both present, newest first relative to each other.
	var iOlder, iNewer = -1, -1
	for i, e := range entries {
		switch e["idn"] {
		case older:
			iOlder = i
		case newer:
			iNewer = i
		}
	}
	if iOlder < 0 || iNewer < 0 {
		t.Fatalf("both referrals should be listed (older=%d newer=%d)", iOlder, iNewer)
	}
	if iNewer > iOlder {
		t.Fatalf("newest-first violated: newer at %d, older at %d", iNewer, iOlder)
	}

	// Tombstone the newer one — it must disappear from the list.
	if err := DeletePerson(d, newer, "supervisor", "test", false); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
	entries, err = ReferralEntries(d)
	if err != nil {
		t.Fatalf("ReferralEntries 2: %v", err)
	}
	for _, e := range entries {
		if e["idn"] == newer {
			t.Fatalf("tombstoned referral %s should be filtered out", newer)
		}
	}
}
