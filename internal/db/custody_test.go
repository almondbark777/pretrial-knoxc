package db

import (
	"testing"

	"pretrial-knoxc/internal/compute"
)

// A custody period added through the app must flow into BuildClients → ComputeGPS
// and reduce the billed GPS days; deleting it restores full billing.
func TestCustodyFlowsIntoGPSBilling(t *testing.T) {
	d := openEnsured(t)
	idn := "999444001"
	if err := AddDefendant(d, NewDefendant{IDN: idn, Name: "ZZCUST, CARL", Level: "2", Status: "Open",
		GPS: "true", GPSType: "ALLIED"}, "by"); err != nil {
		t.Fatalf("AddDefendant: %v", err)
	}
	// GPS install comes from raw_gps_48_hours (gpMap) — seed one so there's a window.
	if _, err := d.Exec(`INSERT INTO raw_gps_48_hours (idn, gps_install_date, gps_type) VALUES (?, '2026-01-01', 'ALLIED')`, idn); err != nil {
		t.Fatalf("seed gps row: %v", err)
	}
	track := compute.Noon(2026, 2, 1) // Jan 1..Feb 1 = 32 days × $8 = $256

	owedAt := func() float64 {
		clients, err := BuildClients(d, track)
		if err != nil {
			t.Fatalf("BuildClients: %v", err)
		}
		cs := clients[idn]
		if len(cs) == 0 {
			t.Fatalf("client %s missing", idn)
		}
		g := compute.ComputeGPS(*cs[0], track, nil, "")
		if g.TotalOwedDollars == nil {
			t.Fatalf("no owed computed: %+v", g)
		}
		return *g.TotalOwedDollars
	}

	if got := owedAt(); got != 256 {
		t.Fatalf("baseline owed = %v, want 256", got)
	}

	// In custody Jan 10 → back Jan 20 = 10 days off → 22 × $8 = $176.
	if err := AddCustodyPeriod(d, idn, "2026-01-10", "2026-01-20", "booked", "by"); err != nil {
		t.Fatalf("AddCustodyPeriod: %v", err)
	}
	periods, err := ListCustodyPeriods(d, idn)
	if err != nil || len(periods) != 1 {
		t.Fatalf("ListCustodyPeriods = %v (%d)", err, len(periods))
	}
	if got := owedAt(); got != 176 {
		t.Fatalf("owed with custody = %v, want 176", got)
	}

	// Remove it → back to full billing.
	if err := DeleteCustodyPeriod(d, periods[0].ID, "by"); err != nil {
		t.Fatalf("DeleteCustodyPeriod: %v", err)
	}
	if got := owedAt(); got != 256 {
		t.Fatalf("owed after delete = %v, want 256", got)
	}
}

// AddCustodyPeriod requires idn + start.
func TestAddCustodyValidation(t *testing.T) {
	d := openEnsured(t)
	if err := AddCustodyPeriod(d, "", "2026-01-01", "", "", "by"); err != errEmptyField {
		t.Fatalf("missing idn err = %v, want errEmptyField", err)
	}
	if err := AddCustodyPeriod(d, "1", "", "", "", "by"); err != errEmptyField {
		t.Fatalf("missing start err = %v, want errEmptyField", err)
	}
}
