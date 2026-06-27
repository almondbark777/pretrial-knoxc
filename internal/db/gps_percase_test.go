package db

import (
	"testing"

	"pretrial-knoxc/internal/compute"
)

// Absher-shaped (problem reports #11 + #12): one idn with a CLOSED GPS case that
// was switched to "no gps", plus two OPEN non-GPS (Plea SCRAM) cases, and TWO
// gps_48_hours rows for the GPS case (a bare install row + a removal row). The
// per-case merge must recover the removal info so (a) the GPS-Monitored tag
// clears on every case (#11) and (b) ComputeGPS stops billing at the switch date
// instead of running to today (#12).
func TestGPSPerCaseRemovalClearsTagAndStopsBilling(t *testing.T) {
	d := freshGPSDB(t)
	idn := "1065438"
	seed := []string{
		`INSERT INTO raw_blue_book (idn, defendant, warrant_case_num, case_status, pretrial_level, gps, gps_type, supervising_officer)
		   VALUES ('1065438','ABSHER, KELLY','@1622784, @1622785','CLOSED','3','True','Allied','Carla Kidwell')`,
		`INSERT INTO raw_blue_book (idn, defendant, warrant_case_num, case_status, pretrial_level, gps, supervising_officer)
		   VALUES ('1065438','ABSHER, KELLY','X@1622784, @1622785','Open','3','False','Carla Kidwell')`,
		`INSERT INTO raw_blue_book (idn, defendant, warrant_case_num, case_status, pretrial_level, gps, gps_type, supervising_officer)
		   VALUES ('1065438','ABSHER, KELLY','@1622785','Open','3','False','allied','Carla Kidwell')`,
		`INSERT INTO raw_gps_48_hours (idn, case_number, gps_type, gps_install_date)
		   VALUES ('1065438','@1622784, @1622785','allied','2026-03-16')`,
		`INSERT INTO raw_gps_48_hours (idn, case_number, gps_type, gps_install_date, switched_to, switched_gps_date)
		   VALUES ('1065438','@1622784, @1622785','Allied','2026-03-16','no gps','2026-05-11')`,
		`INSERT INTO raw_payments (idn, case_number, payment_type, payment_amount, payment_date)
		   VALUES ('1065438','@1622784, @1622785','Allied','464','2026-03-11T04:00:00Z')`,
	}
	for _, s := range seed {
		if _, err := d.Exec(s); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	track := compute.Noon(2026, 6, 26)
	clients, err := BuildClients(d, track)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	cases := clients[idn]
	if len(cases) != 3 {
		t.Fatalf("want 3 case rows, got %d", len(cases))
	}
	// #11: the GPS-Monitored tag must be cleared on every case (removed / off GPS).
	for _, c := range cases {
		if c.GpsActive {
			t.Errorf("case %q GpsActive = true, want false (removed / off GPS)", c.CaseNo)
		}
	}
	// The GPS case must carry the recovered removal info from the second GPS row.
	var gpsCase *compute.Client
	for _, c := range cases {
		if compute.CaseMatches(c.CaseNo, "@1622784") {
			gpsCase = c
			break
		}
	}
	if gpsCase == nil {
		t.Fatal("GPS case (@1622784) not found")
	}
	if !compute.IsReliefSwitch(gpsCase.GpSwitchedTo) {
		t.Fatalf("switched_to not recovered from the removal row: %q", gpsCase.GpSwitchedTo)
	}
	// #12: billing stops at 5/11 (Mar16..May11 incl = 57 days × $8 = $456), not at
	// today. Paid $464 → paid up, not behind.
	g := compute.ComputeGPS(*gpsCase, track, nil, gpsCase.CaseNo)
	if g.TotalOwedDollars == nil || *g.TotalOwedDollars != 456 {
		t.Fatalf("owed = %v, want 456 (billing stops at removal, not today)", g.TotalOwedDollars)
	}
	if g.SurplusDollars == nil || *g.SurplusDollars < 0 {
		t.Fatalf("surplus = %v, want >= 0 (paid up through removal)", g.SurplusDollars)
	}
}

// Control: a single-record GPS client (the common case) is unchanged by the
// per-case refactor — still GPS-active and billed to the track date.
func TestGPSPerCaseSingleRecordUnchanged(t *testing.T) {
	d := freshGPSDB(t)
	idn := "200001"
	seed := []string{
		`INSERT INTO raw_blue_book (idn, defendant, warrant_case_num, case_status, pretrial_level, gps, gps_type)
		   VALUES ('200001','SOLO, SAM','@900','Open','2','True','ALLIED')`,
		`INSERT INTO raw_gps_48_hours (idn, case_number, gps_type, gps_install_date)
		   VALUES ('200001','@900','ALLIED','2026-01-01')`,
	}
	for _, s := range seed {
		if _, err := d.Exec(s); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	track := compute.Noon(2026, 2, 1) // Jan1..Feb1 = 32 days × $8 = $256
	clients, err := BuildClients(d, track)
	if err != nil {
		t.Fatalf("BuildClients: %v", err)
	}
	cs := clients[idn]
	if len(cs) != 1 || !cs[0].GpsActive {
		t.Fatalf("single-record GPS client should be active; got %d cases active=%v", len(cs), len(cs) == 1 && cs[0].GpsActive)
	}
	g := compute.ComputeGPS(*cs[0], track, nil, "")
	if g.TotalOwedDollars == nil || *g.TotalOwedDollars != 256 {
		t.Fatalf("owed = %v, want 256 (billed to track, unchanged)", g.TotalOwedDollars)
	}
}

// #11 manual backstop: an explicit "gps_removed" override (set via the Edit-GPS
// form) clears the GPS-Monitored tag even when the import has no removal row.
func TestGPSRemovedOverrideClearsTag(t *testing.T) {
	d := freshGPSDB(t)
	idn := "300001"
	if _, err := d.Exec(`INSERT INTO raw_blue_book (idn, defendant, warrant_case_num, case_status, gps, gps_type)
		VALUES ('300001','OFF, OLIVER','@700','Open','True','ALLIED')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	track := compute.Noon(2026, 2, 1)
	clients, _ := BuildClients(d, track)
	if !clients[idn][0].GpsActive {
		t.Fatal("should start GPS-active")
	}
	if err := SetGPSDetails(d, idn, map[string]string{"gps_removed": "true"}, "tester"); err != nil {
		t.Fatalf("SetGPSDetails: %v", err)
	}
	clients, _ = BuildClients(d, track)
	if clients[idn][0].GpsActive {
		t.Fatal("gps_removed override should clear GpsActive")
	}
}

// #12 manual backstop: SetNotBehind sets Client.NotBehind (consumed by
// behindRoster to hold the person off the roster); ClearNotBehind reverts it.
func TestNotBehindFlagFlow(t *testing.T) {
	d := freshGPSDB(t)
	idn := "400001"
	if _, err := d.Exec(`INSERT INTO raw_blue_book (idn, defendant, warrant_case_num, case_status, gps, gps_type)
		VALUES ('400001','REVIEW, RHEA','@800','Open','True','ALLIED')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	track := compute.Noon(2026, 2, 1)
	clients, _ := BuildClients(d, track)
	if clients[idn][0].NotBehind {
		t.Fatal("should start not-acked")
	}
	if err := SetNotBehind(d, idn, "paid off-system", "tester"); err != nil {
		t.Fatalf("SetNotBehind: %v", err)
	}
	if !HasNotBehindAck(d, idn) {
		t.Fatal("HasNotBehindAck should be true after set")
	}
	clients, _ = BuildClients(d, track)
	if !clients[idn][0].NotBehind {
		t.Fatal("NotBehind flag should be set on the client after ack")
	}
	if err := ClearNotBehind(d, idn, "tester"); err != nil {
		t.Fatalf("ClearNotBehind: %v", err)
	}
	clients, _ = BuildClients(d, track)
	if clients[idn][0].NotBehind {
		t.Fatal("NotBehind flag should clear after ClearNotBehind")
	}
}
