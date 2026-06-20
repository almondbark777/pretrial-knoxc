package db

import (
	"database/sql"
	"sort"
)

// referrals.go reads the app-entered referrals (added_defendants — the rows the
// console "New Client Referral" wizard writes) for the spreadsheet-style
// Referrals view. Bulk SharePoint-imported clients live in raw_blue_book and are
// shown on the Clients roster; this is specifically the data officers keyed in
// through the app, with every captured field visible like a SharePoint list.

// ReferralEntries returns every app-entered referral as a column->value map,
// newest first (by created_at). Whole-person tombstones and per-case suppressions
// are filtered, so a supervisor-deleted referral disappears here too — the same
// guarantee every other view honors.
func ReferralEntries(d *sql.DB) ([]map[string]string, error) {
	rows, err := queryMapsIfExists(d, "added_defendants")
	if err != nil {
		return nil, err
	}
	tomb, err := loadTombstones(d)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]string, 0, len(rows))
	for _, r := range rows {
		idn := norm(r["idn"])
		if idn != "" && tomb.whole[idn] {
			continue
		}
		if idn != "" && tomb.caseSuppressed(idn, firstNonEmpty(r["warrant_case_num"], r["case_number"])) {
			continue
		}
		out = append(out, r)
	}
	// Newest first. created_at is an ISO-ish "YYYY-MM-DD HH:MM:SS" stamp, so a
	// plain string compare orders chronologically; ties fall back to add_id.
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := out[i]["created_at"], out[j]["created_at"]
		if ci != cj {
			return ci > cj
		}
		return out[i]["add_id"] > out[j]["add_id"]
	})
	return out, nil
}
