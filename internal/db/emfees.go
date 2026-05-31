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
	if extra, err := queryMapsIfExists(d, "added_defendants"); err != nil {
		return emfees.Result{}, err
	} else {
		blueBook = append(blueBook, extra...)
	}

	// Splice supervisor field overrides into the blue-book rows by idn — the same
	// correction every other view sees (BuildClients/LookupDatasets) — so the fee
	// math and the show-cause letters reflect a fixed GPS type/rate, name, case
	// status, or referral/closed date. Overridable fields are blue-book columns, so
	// only blueBook is spliced; the 48-hour and payment sources are left as-is.
	overrides, err := loadOverrides(d)
	if err != nil {
		return emfees.Result{}, err
	}
	for _, r := range blueBook {
		for f, v := range overrides[norm(r["idn"])] {
			r[f] = v
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

	return emfees.Compute(gps48, payments, blueBook, asOf), nil
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
