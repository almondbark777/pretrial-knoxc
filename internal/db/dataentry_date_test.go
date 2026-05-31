package db

import (
	"testing"
	"time"
)

// TestNormInputDate covers the ISO -> M/D/YYYY conversion that keeps app-entered
// dates consistent with the imported SharePoint data (and parseable by the
// M/D/YYYY-only EM-fee engine). Non-ISO input is returned trimmed + unchanged, so
// the helper is safe to apply to every caller.
func TestNormInputDate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2026-05-01", "5/1/2026"},   // browser date picker -> US, unpadded
		{"2026-12-25", "12/25/2026"}, // two-digit month/day
		{" 2026-01-09 ", "1/9/2026"}, // trimmed first
		{"5/1/2026", "5/1/2026"},     // already canonical -> unchanged (idempotent)
		{"", ""},                     // blank -> blank
		{"not a date", "not a date"}, // junk -> unchanged (flexible parsers decide)
		{"2026-13-40", "2026-13-40"}, // impossible date -> left as-is
	}
	for _, c := range cases {
		if got := normInputDate(c.in); got != c.want {
			t.Errorf("normInputDate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestAddPaymentRejectsBadAmount guards the silent-$0 trap: a non-numeric or
// non-positive amount is rejected up front rather than stored as a payment that
// credits nothing toward the fee math. Valid amounts (incl. "$" / "," / decimals)
// are accepted.
func TestAddPaymentRejectsBadAmount(t *testing.T) {
	d := openEnsured(t)
	idn := "999000555"
	if err := AddDefendant(d, NewDefendant{IDN: idn, Name: "ZZAMT, AL", Status: "Open"}, "by"); err != nil {
		t.Fatalf("AddDefendant: %v", err)
	}
	for _, bad := range []string{"fifty", "abc", "0", "-5", "$"} {
		if err := AddPayment(d, idn, "", "5/1/2026", bad, "GPS", "", "by"); err != errBadAmount {
			t.Fatalf("AddPayment(amount=%q) err = %v, want errBadAmount", bad, err)
		}
	}
	for _, ok := range []string{"50", "50.00", "$1,200.50", "8"} {
		if err := AddPayment(d, idn, "", "5/1/2026", ok, "GPS", "", "by"); err != nil {
			t.Fatalf("AddPayment(amount=%q) err = %v, want nil", ok, err)
		}
	}
}

// TestAddedPaymentDateReachesEMFees proves the real-world bug is fixed: a payment
// entered through the website's date picker (ISO YYYY-MM-DD) is stored canonically
// and is fully understood by the EM-fee engine. The discriminator is StartSrc — the
// payment's *amount* always counts, but its *date* only anchors the "First payment"
// start source if it actually parsed. Before the fix (ISO stored verbatim) the date
// was unparseable and the start stayed "Referral".
func TestAddedPaymentDateReachesEMFees(t *testing.T) {
	d := freshEMFeesDB(t)
	idn := "800000001"
	addBBClosed(t, d, idn, "DELTA, DON", "@D1", "ALLIED") // referral 1/1/2026, closed 2/1/2026
	asOf := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)

	// Baseline: no payments -> start derives from the referral date.
	base, err := EMFees(d, asOf)
	if err != nil {
		t.Fatalf("EMFees baseline: %v", err)
	}
	if len(base.Closed) != 1 || base.Closed[0].StartSrc != "Referral" || base.Closed[0].Paid != 0 {
		t.Fatalf("baseline: want 1 closed rec, Referral start, $0 paid; got %+v", base.Closed)
	}

	// Record a GPS payment via the website using a browser-style ISO date.
	if err := AddPayment(d, idn, "", "2026-01-15", "8", "GPS", "Officer Z", "by@knoxsheriff.org"); err != nil {
		t.Fatalf("AddPayment: %v", err)
	}
	// Stored canonically (not the raw ISO string).
	pays, err := ListAddedPayments(d, idn)
	if err != nil || len(pays) != 1 {
		t.Fatalf("ListAddedPayments = %v, %v", pays, err)
	}
	if pays[0].PaymentDate != "1/15/2026" {
		t.Fatalf("stored payment date = %q, want canonical 1/15/2026", pays[0].PaymentDate)
	}

	// After: the engine sees both the amount AND the date.
	after, err := EMFees(d, asOf)
	if err != nil {
		t.Fatalf("EMFees after payment: %v", err)
	}
	if len(after.Closed) != 1 {
		t.Fatalf("after: want 1 closed rec, got %d (%+v)", len(after.Closed), after.Closed)
	}
	rec := after.Closed[0]
	if rec.Paid != 8 {
		t.Fatalf("after: paid = %v, want 8 (the ISO-dated payment's amount must count)", rec.Paid)
	}
	if rec.StartSrc != "First payment" {
		t.Fatalf("after: StartSrc = %q, want \"First payment\" — the ISO date must parse in the engine", rec.StartSrc)
	}
	wantStart := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	if !rec.Start.Equal(wantStart) {
		t.Fatalf("after: Start = %v, want %v", rec.Start, wantStart)
	}
}
