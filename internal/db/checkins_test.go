package db

import (
	"testing"

	"pretrial-knoxc/internal/models"
)

// sampleCheckin is a minimal valid submission for tests.
func sampleCheckin(idn, name, badge string) models.Checkin {
	return models.Checkin{
		IDN:           idn,
		ReportType:    "Pretrial",
		ClientName:    name,
		ServerTS:      "2026-06-25 09:00:00 EST",
		SrcIP:         "203.0.113.7",
		GPSPerm:       "granted",
		GPSLat:        35.9646,
		GPSLng:        -83.9202,
		PresenceBadge: badge,
	}
}

// The headline guarantees: an insert hash-chains off the prior row, the chain
// verifies, the queue is FIFO, approve/reject only stamp review columns (the
// captured data is untouched), and a post-hoc edit is detectable.
func TestCheckinInsertChainAndReview(t *testing.T) {
	d := openEnsured(t)

	id1, h1, err := InsertCheckin(d, sampleCheckin("100200300", "Alpha Tester", "green"))
	if err != nil {
		t.Fatalf("insert 1: %v", err)
	}
	id2, h2, err := InsertCheckin(d, sampleCheckin("100200301", "Bravo Tester", "red"))
	if err != nil {
		t.Fatalf("insert 2: %v", err)
	}
	if h1 == "" || h2 == "" || h1 == h2 {
		t.Fatalf("hashes should be present and distinct: %q / %q", h1, h2)
	}

	// Second row chains off the first.
	c2, err := GetCheckin(d, id2)
	if err != nil || c2 == nil {
		t.Fatalf("get 2: %v", err)
	}
	if c2.PrevHash != h1 {
		t.Errorf("row 2 prev_hash = %q, want %q (row 1 hash)", c2.PrevHash, h1)
	}

	// Chain verifies clean.
	if bad, err := VerifyCheckinChain(d); err != nil || bad != 0 {
		t.Fatalf("VerifyCheckinChain = (%d, %v), want (0, nil)", bad, err)
	}

	// FIFO queue: oldest first, both pending.
	pend, err := ListPendingCheckins(d)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pend) != 2 || pend[0].ID != id1 || pend[1].ID != id2 {
		t.Fatalf("pending order = %v, want [%d %d]", idsOf(pend), id1, id2)
	}
	if n, _ := CountPendingCheckins(d); n != 2 {
		t.Errorf("CountPendingCheckins = %d, want 2", n)
	}

	// Approve row 1 → leaves the queue, captured data unchanged.
	if err := ApproveCheckin(d, id1, "officer@knoxsheriff.org"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	c1, _ := GetCheckin(d, id1)
	if c1.Status != "approved" || c1.ApprovedBy != "officer@knoxsheriff.org" || c1.ApprovedAt == "" {
		t.Errorf("after approve: status=%q by=%q at=%q", c1.Status, c1.ApprovedBy, c1.ApprovedAt)
	}
	if c1.ClientName != "Alpha Tester" || c1.RecordHash != h1 {
		t.Error("approve mutated captured data or hash")
	}

	// Reject row 2 with a reason.
	if err := RejectCheckin(d, id2, "officer@knoxsheriff.org", "pinged client's home address"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	c2, _ = GetCheckin(d, id2)
	if c2.Status != "rejected" || c2.RejectReason != "pinged client's home address" {
		t.Errorf("after reject: status=%q reason=%q", c2.Status, c2.RejectReason)
	}

	if n, _ := CountPendingCheckins(d); n != 0 {
		t.Errorf("pending after review = %d, want 0", n)
	}

	// Tamper a sealed row directly → the chain must catch it.
	if _, err := d.Exec(`UPDATE checkins SET client_name = 'EDITED' WHERE checkin_id = ?`, id1); err != nil {
		t.Fatalf("tamper exec: %v", err)
	}
	if bad, _ := VerifyCheckinChain(d); bad != id1 {
		t.Errorf("VerifyCheckinChain after tamper = %d, want %d", bad, id1)
	}
}

func idsOf(cs []models.Checkin) []int64 {
	out := make([]int64, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}

// GPS is stored only when granted, so a denied submission records NULL distance
// rather than a misleading 0,0.
func TestCheckinGpsNullWhenDenied(t *testing.T) {
	d := openEnsured(t)
	c := sampleCheckin("100200400", "Charlie Tester", "yellow")
	c.GPSPerm = "denied"
	c.GPSLat, c.GPSLng = 0, 0
	id, _, err := InsertCheckin(d, c)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	var lat any
	if err := d.QueryRow(`SELECT gps_lat FROM checkins WHERE checkin_id = ?`, id).Scan(&lat); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if lat != nil {
		t.Errorf("gps_lat = %v, want NULL when permission denied", lat)
	}
}

func TestClientContactUpsertPreservesGeo(t *testing.T) {
	d := openEnsured(t)
	const idn = "555444333"

	// First write with a geocoded home.
	c := models.ClientContact{IDN: idn, PhoneE164: "+18655551234", AddressLine1: "123 Main St", HomeLat: 35.96, HomeLng: -83.92}
	if err := UpsertClientContact(d, c, true, "officer@knoxsheriff.org"); err != nil {
		t.Fatalf("upsert 1: %v", err)
	}
	// Second write changes the phone but does NOT supply geo — geo must survive.
	c2 := models.ClientContact{IDN: idn, PhoneE164: "+18655559999", AddressLine1: "123 Main St"}
	if err := UpsertClientContact(d, c2, false, "officer@knoxsheriff.org"); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	got, err := GetClientContact(d, idn)
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.PhoneE164 != "+18655559999" {
		t.Errorf("phone = %q, want updated", got.PhoneE164)
	}
	if !got.HasHomeGeo || got.HomeLat == 0 {
		t.Errorf("home geo lost on phone-only update: %+v", got)
	}
}

func TestWeeklyCodeRotation(t *testing.T) {
	d := openEnsured(t)
	if _, err := CreateWeeklyCode(d, "ABC111", "Week of Jun 15", "2026-06-15", "2026-06-21", "sup@knoxsheriff.org"); err != nil {
		t.Fatalf("create 1: %v", err)
	}
	if _, err := CreateWeeklyCode(d, "ABC222", "Week of Jun 22", "2026-06-22", "2026-06-28", "sup@knoxsheriff.org"); err != nil {
		t.Fatalf("create 2: %v", err)
	}
	active, err := ActiveWeeklyCode(d)
	if err != nil || active == nil {
		t.Fatalf("active: %v", err)
	}
	if active.Code != "ABC222" {
		t.Errorf("active code = %q, want ABC222 (newest)", active.Code)
	}
	// The prior code is still resolvable (so a stale-code submission can be flagged).
	old, _ := WeeklyCodeByCode(d, "ABC111")
	if old == nil || old.Active {
		t.Errorf("old code = %+v, want present and inactive", old)
	}
}

func TestCheckinConfigDefaultAndSet(t *testing.T) {
	d := openEnsured(t)
	// Built-in defaults present before anything is written — both capabilities off.
	if v := GetCheckinConfig(d, "sms_otp_enabled"); v != "0" {
		t.Errorf("sms_otp_enabled default = %q, want 0", v)
	}
	if v := GetCheckinConfig(d, "background_location_enabled"); v != "0" {
		t.Errorf("background_location_enabled default = %q, want 0", v)
	}
	if err := SetCheckinConfig(d, "sms_otp_enabled", "1", "admin@knoxsheriff.org"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v := GetCheckinConfig(d, "sms_otp_enabled"); v != "1" {
		t.Errorf("after set = %q, want 1", v)
	}
}
