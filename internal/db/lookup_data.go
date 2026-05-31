// lookup_data.go reimplements the Python app_lookup's /api/lookup_data feed in
// Go, so the single binary can serve the existing bundled "PTR Client Lookup"
// tracker as the landing page. It returns the four datasets (bb/ci/pm/gp) with
// raw_* columns renamed back to the exact SharePoint headers the tracker's
// colFind() expects, every value coerced to a string (mirrors PapaParse).
//
// It honors the SAME suppression + corrections as BuildClients: tombstoned idns
// (and cases) are filtered, and supervisor overrides are spliced in — so a
// deleted person disappears from the tracker too, and a corrected field shows
// the corrected value there. (Phase 7 guarantee: gone/changed in EVERY view.)
package db

import "database/sql"

// SharePoint-header maps (snake_case raw column -> exact header), ported verbatim
// from webapp/queries_ext.py so the bundled tracker consumes the feed unchanged.
var (
	bbHeaderMap = map[string]string{
		"idn": "IDN", "defendant": "Defendant", "case_status": "Case Status",
		"pretrial_level": "Pretrial Level ", "supervising_officer": "Supervising Officer",
		"supervision_type": "Supervision Type", "charge_type": "Charge Type",
		"order_from": "Order From", "referral_date": "Referral Date", "closed_date": "Closed Date",
		"bond_amount": "Bond Amount", "gps": "GPS", "gps_type": "GPS Type", "dma": "DMA",
		"birthdate": "Birthdate", "warrant_case_num": "Warrant/Case #",
		"ptr_successfully_completed": "PTR Successfully Completed?", "victim": "Victim",
		"day_adjustment": "Day Adjustment",
	}
	ciHeaderMap = map[string]string{
		"idn": "IDN", "case_number": "Case Number", "defendant": "Defendant",
		"date": "Check in Date", "type_of_check_in": "Type of check in",
		"supervising_officer": "Supervising Officer", "case_status": "Case Status",
		"referral_date": "Referral Date", "pretrial_level": "Pretrial Level ",
	}
	pmHeaderMap = map[string]string{
		"idn": "IDN", "case_number": "Case Number", "defendant": "Defendant",
		"payment_date": "Payment Date", "payment_amount": "Payment Amount",
		"officer_that_collected_payment": "Officer That Collected Payment",
		"payment_type":                   "Payment Type", "case_status": "Case Status",
	}
	gpHeaderMap = map[string]string{
		"idn": "IDN", "case_number": "Case Number", "defendant": "Defendant",
		"referral_date": "Referral Date", "gps_type": "GPS Type", "case_status": "Case Status",
		"paid": "Paid", "victim": "Victim", "victim_accept_deny_gps": "Victim Accept/Deny GPS",
		"gps_install_date": "GPS Install Date", "order": "Order", "da_emailed": "DA Emailed",
		"closed_date": "Closed Date", "switched_to": "Switched To",
		"switched_gps_date": "Switched GPS Date", "notes": "Notes",
	}
)

// LookupDatasets returns {"bb":[...],"ci":[...],"pm":[...],"gp":[...]} for the
// tracker. Whole-person tombstones drop the idn from every dataset; per-case
// tombstones drop the matching blue_book rows; overrides are applied to bb.
func LookupDatasets(d *sql.DB) (map[string]any, error) {
	tomb, err := loadTombstones(d)
	if err != nil {
		return nil, err
	}
	overrides, err := loadOverrides(d)
	if err != nil {
		return nil, err
	}

	bbRaw, err := queryMaps(d, "raw_blue_book")
	if err != nil {
		return nil, err
	}
	ciRaw, err := queryMaps(d, "raw_check_ins")
	if err != nil {
		return nil, err
	}
	pmRaw, err := queryMaps(d, "raw_payments")
	if err != nil {
		return nil, err
	}
	gpRaw, err := queryMaps(d, "raw_gps_48_hours")
	if err != nil {
		return nil, err
	}

	// Blue book: per-row suppression + overrides, then remap.
	bb := make([]map[string]string, 0, len(bbRaw))
	for _, r := range bbRaw {
		idn := norm(r["idn"])
		if idn == "" || tomb.whole[idn] {
			continue
		}
		if tomb.caseSuppressed(idn, firstNonEmpty(r["warrant_case_num"], r["case_number"])) {
			continue
		}
		for f, v := range overrides[idn] {
			r[f] = v
		}
		bb = append(bb, remap(r, bbHeaderMap))
	}

	return map[string]any{
		"bb": bb,
		"ci": remapAll(ciRaw, ciHeaderMap, tomb),
		"pm": remapAll(pmRaw, pmHeaderMap, tomb),
		"gp": remapAll(gpRaw, gpHeaderMap, tomb),
	}, nil
}

// remap renames one raw row to SharePoint headers, emitting EVERY mapped header
// (absent column -> "") so the tracker's colFind sees a complete, stable schema.
func remap(r map[string]string, m map[string]string) map[string]string {
	o := make(map[string]string, len(m))
	for col, header := range m {
		o[header] = r[col]
	}
	return o
}

// remapAll remaps a whole dataset, dropping rows for whole-person-tombstoned idns
// (ci/pm/gp are person-scoped; per-case suppression only trims blue_book rows,
// matching BuildClients' join semantics).
func remapAll(rows []map[string]string, m map[string]string, tomb tombstoneSets) []map[string]string {
	out := make([]map[string]string, 0, len(rows))
	for _, r := range rows {
		if tomb.whole[norm(r["idn"])] {
			continue
		}
		out = append(out, remap(r, m))
	}
	return out
}
