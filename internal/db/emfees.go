package db

import (
	"database/sql"
	"time"

	"pretrial-knoxc/internal/emfees"
)

// EMFees computes the past-due EM-fee records straight from the raw_* tables (the
// same source the canonical skill reads from its three CSVs — those CSVs ARE what
// the daily importer loads into these tables). Tombstoned people/cases are dropped
// first, so a supervisor-deleted person never generates a dunning letter — the same
// importer-proof guarantee BuildClients enforces for every other view.
//
// Supervisor field overrides ARE spliced into the blue-book rows here (by idn,
// exactly as BuildClients/LookupDatasets do), so a corrected GPS type (which sets
// the $8 ALLIED / $15 SCRAM rate), name (the junk-name filter and the letter), case
// status (the Open/Closed split) or referral/closed date (the billing window) flows
// into the fee math and the generated show-cause letter — keeping this report and
// its legally-meaningful letters consistent with every other view.
func EMFees(d *sql.DB, asOf time.Time) (emfees.Result, error) {
	gps48, err := queryMaps(d, "raw_gps_48_hours")
	if err != nil {
		return emfees.Result{}, err
	}
	payments, err := queryMaps(d, "raw_payments")
	if err != nil {
		return emfees.Result{}, err
	}
	blueBook, err := queryMaps(d, "raw_blue_book")
	if err != nil {
		return emfees.Result{}, err
	}

	// Merge app-added payments + defendants (Phase 10) so an officer-entered GPS
	// payment reduces the arrears here, and an app-added person is considered.
	if extra, err := queryMapsIfExists(d, "added_payments"); err != nil {
		return emfees.Result{}, err
	} else {
		payments = append(payments, extra...)
	}
	addedDefs, err := queryMapsIfExists(d, "added_defendants")
	if err != nil {
		return emfees.Result{}, err
	}
	blueBook = append(blueBook, addedDefs...)

	// #2 — App-entered OPEN GPS clients must also appear in the arrears / show-cause
	// letters. The 48-hour file (Pass 1) is the live SharePoint export, so an
	// officer-added person never has a 48-hour row and would silently miss a letter.
	// For each added_defendants row that carries a GPS install date AND whose IDN is
	// not already represented in the 48-hour file, synthesize a gps48-shaped row and
	// append it before the tombstone filter. The "not already in gps48" guard is what
	// prevents double-billing: if the importer later ships a real 48-hour row for the
	// same person, the synthetic one is suppressed so Pass 1 bills them exactly once.
	gps48IDNs := map[string]bool{}
	for _, r := range gps48 {
		if idn := norm(r["idn"]); idn != "" {
			gps48IDNs[idn] = true
		}
	}
	for _, r := range addedDefs {
		idn := norm(r["idn"])
		if idn == "" || gps48IDNs[idn] {
			continue
		}
		if norm(r["gps_install_date"]) == "" {
			continue // not a GPS install — nothing to bill
		}
		gps48 = append(gps48, syntheticGPS48(r))
		gps48IDNs[idn] = true // guard against >1 added row for the same IDN
	}

	// Splice supervisor field overrides into the blue-book rows by idn — the same
	// correction every other view sees (BuildClients/LookupDatasets) — so the fee
	// math and the show-cause letters reflect a fixed GPS type/rate, name, case
	// status, or referral/closed date.
	overrides, err := loadOverrides(d)
	if err != nil {
		return emfees.Result{}, err
	}
	for _, r := range blueBook {
		for f, v := range overrides[norm(r["idn"])] {
			r[f] = v
		}
	}
	// #1 — Splice overrides into the 48-hour (Pass 1) rows too, by IDN, limited to
	// the three fields that change the fee math / Open-Closed split: a corrected GPS
	// type sets the $8 ALLIED / $15 SCRAM rate, case_status flips the Open/Closed
	// list, and closed_date moves the billing-window end. Without this, a supervisor's
	// SCRAM→ALLIED correction (or a status/date fix) is silently discarded for the
	// entire Open list, since Pass 1 reads only the raw 48-hour rows. The 48-hour
	// payment/switch columns are NOT touched (overrides only carry blue-book fields).
	for _, r := range gps48 {
		ov := overrides[norm(r["idn"])]
		if ov == nil {
			continue
		}
		for _, f := range []string{"gps_type", "case_status", "closed_date"} {
			if v, ok := ov[f]; ok {
				r[f] = v
			}
		}
	}

	tomb, err := loadTombstones(d)
	if err != nil {
		return emfees.Result{}, err
	}
	gps48 = filterTomb(gps48, tomb, func(r map[string]string) string { return norm(r["case_number"]) })
	payments = filterTomb(payments, tomb, func(r map[string]string) string { return norm(r["case_number"]) })
	blueBook = filterTomb(blueBook, tomb, func(r map[string]string) string {
		return norm(firstNonEmpty(r["warrant_case_num"], r["case_number"]))
	})

	// In-custody days are excluded from each person's GPS arrearage (the "back on
	// GPS"/reinstall day is billed), so a stretch in jail lowers — or clears — the
	// show-cause letter, matching the console GPS card.
	custody, err := loadCustodyForEMFees(d)
	if err != nil {
		return emfees.Result{}, err
	}

	return emfees.ComputeWithCustody(gps48, payments, blueBook, custody, asOf), nil
}

// syntheticGPS48 maps an added_defendants row into the gps48 row shape Pass 1 reads,
// so an app-entered OPEN GPS client appears on the arrears / show-cause list. The key
// remap is warrant_case_num → case_number (the 48-hour file's case column); the GPS
// install/switch/status/date columns share the same snake_case names and pass through.
// An added person with no recorded status defaults to OPEN (the common intake case),
// matching the "app-entered OPEN GPS clients" the fix targets.
func syntheticGPS48(r map[string]string) map[string]string {
	status := norm(r["case_status"])
	if status == "" {
		status = "OPEN"
	}
	return map[string]string{
		"idn":               norm(r["idn"]),
		"defendant":         r["defendant"],
		"case_number":       firstNonEmpty(r["warrant_case_num"], r["case_number"]),
		"case_status":       status,
		"gps_type":          r["gps_type"],
		"gps_install_date":  r["gps_install_date"],
		"closed_date":       r["closed_date"],
		"switched_to":       r["switched_to"],
		"switched_gps_date": r["switched_gps_date"],
	}
}

// filterTomb drops rows for whole-person tombstones and for the specific suppressed
// case (caseOf extracts the row's case number, since the column differs per table).
func filterTomb(rows []map[string]string, tomb tombstoneSets, caseOf func(map[string]string) string) []map[string]string {
	out := rows[:0:0]
	for _, r := range rows {
		idn := norm(r["idn"])
		if idn != "" && tomb.whole[idn] {
			continue
		}
		if idn != "" && tomb.caseSuppressed(idn, caseOf(r)) {
			continue
		}
		out = append(out, r)
	}
	return out
}
