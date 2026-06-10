// waivers.go — supervisor-granted GPS fee waivers (the console record's
// "Waive GPS fees" action). The vendor's GPS notes already carry historical
// waivers as free text, matched everywhere by compute.IsFeesWaived; rather
// than teach every consumer a second flag, an app waiver is spliced into
// gp_notes at the two read points (BuildClients + LookupDatasets) as a marker
// IsFeesWaived already matches — so the record chip, the rosters' Waived
// flag, and the bundled tracker all light up through the existing
// single-source-of-truth math. Same shape as the other extension tables:
// app-owned (importer-proof), one audit_log breadcrumb per mutation in the
// same transaction, purged on whole-person delete (extensionTablesByIDN).
package db

import (
	"database/sql"
	"strings"

	"pretrial-knoxc/internal/compute"
)

// loadFeeWaivers returns idn -> display marker for every app fee waiver.
// Tolerant of a DB without the table (empty map) so read paths stay usable on
// a snapshot that predates migration 006.
func loadFeeWaivers(d *sql.DB) (map[string]string, error) {
	out := map[string]string{}
	if !tableExists(d, "fee_waivers") {
		return out, nil
	}
	rows, err := d.Query(`SELECT idn, COALESCE(reason,''), COALESCE(waived_by,''), created_at FROM fee_waivers`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var idn, reason, by, created string
		if err := rows.Scan(&idn, &reason, &by, &created); err != nil {
			return out, err
		}
		if idn = strings.TrimSpace(idn); idn != "" {
			out[idn] = waiverMarker(by, created, reason)
		}
	}
	return out, rows.Err()
}

// waiverMarker renders one waiver as the gp_notes splice text. It must keep
// matching compute.IsFeesWaived (/waiv/i + /(fee|gps|payment|charge)/i).
func waiverMarker(by, created, reason string) string {
	m := "GPS fees waived (app"
	if n := compute.FmtOfficer(strings.TrimSpace(by)); n != "" {
		m += " — " + n
	}
	if len(created) >= 10 {
		m += " " + created[:10]
	}
	m += ")"
	if r := strings.TrimSpace(reason); r != "" {
		m += ": " + r
	}
	return m
}

// appendGpNote joins an existing notes value and the waiver marker.
func appendGpNote(notes, marker string) string {
	if strings.TrimSpace(notes) == "" {
		return marker
	}
	return notes + " | " + marker
}

// HasFeeWaiver reports whether an app waiver exists for this idn. A waiver
// living only in the vendor's notes doesn't count — that text is raw data the
// app can't clear, so the record menu must not offer to remove it.
func HasFeeWaiver(d *sql.DB, idn string) bool {
	if !tableExists(d, "fee_waivers") {
		return false
	}
	var one int
	err := d.QueryRow(`SELECT 1 FROM fee_waivers WHERE idn = ?`, strings.TrimSpace(idn)).Scan(&one)
	return err == nil
}

// SetFeeWaiver grants a GPS fee waiver (re-granting replaces the reason).
// One audited transaction.
func SetFeeWaiver(d *sql.DB, idn, reason, by string) error {
	idn, by, reason = strings.TrimSpace(idn), strings.TrimSpace(by), strings.TrimSpace(reason)
	if idn == "" || by == "" {
		return errEmptyField
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Upsert by idn (UNIQUE) so a re-grant refreshes reason/by/stamp.
	if _, err := tx.Exec(`DELETE FROM fee_waivers WHERE idn = ?`, idn); err != nil {
		return err
	}
	ts := compute.NowET().Format("2006-01-02 15:04:05 MST")
	if _, err := tx.Exec(
		`INSERT INTO fee_waivers (idn, reason, waived_by, created_at) VALUES (?, ?, ?, ?)`,
		idn, nz(reason), by, ts); err != nil {
		return err
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: "waiver_add", Table: "fee_waivers", RowID: idn, NewValue: reason,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// ClearFeeWaiver removes an app waiver. Clearing an idn without one is a
// no-op (no audit row), so a stale double-submit can't litter the log.
func ClearFeeWaiver(d *sql.DB, idn, by string) error {
	idn, by = strings.TrimSpace(idn), strings.TrimSpace(by)
	if idn == "" || by == "" {
		return errEmptyField
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM fee_waivers WHERE idn = ?`, idn)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return tx.Commit()
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: "waiver_remove", Table: "fee_waivers", RowID: idn,
	}); err != nil {
		return err
	}
	return tx.Commit()
}
