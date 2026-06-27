// notbehind.go — "Reviewed — not behind" acknowledgements (problem report #12).
// An officer can review a Behind-on-GPS flag and confirm the person is NOT
// actually behind (e.g. paid off-system, or a data edge the import didn't
// capture), holding them off the compliance Behind roster. This is DISTINCT
// from a fee waiver, which forgives a genuine debt — here we assert the debt
// isn't real. App-owned (importer-proof), one audit_log breadcrumb per mutation.
package db

import (
	"database/sql"
	"strings"

	"pretrial-knoxc/internal/compute"
)

// loadNotBehindAcks returns the set of idns marked "reviewed — not behind".
// Tolerant of a DB without the table (empty set) so read paths stay usable on a
// snapshot that predates the table.
func loadNotBehindAcks(d *sql.DB) (map[string]bool, error) {
	out := map[string]bool{}
	if !tableExists(d, "not_behind_acks") {
		return out, nil
	}
	rows, err := d.Query(`SELECT idn FROM not_behind_acks`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var idn string
		if err := rows.Scan(&idn); err != nil {
			return out, err
		}
		if idn = strings.TrimSpace(idn); idn != "" {
			out[idn] = true
		}
	}
	return out, rows.Err()
}

// HasNotBehindAck reports whether an idn is currently held off the Behind roster.
func HasNotBehindAck(d *sql.DB, idn string) bool {
	if !tableExists(d, "not_behind_acks") {
		return false
	}
	var one int
	err := d.QueryRow(`SELECT 1 FROM not_behind_acks WHERE idn = ?`, strings.TrimSpace(idn)).Scan(&one)
	return err == nil
}

// SetNotBehind marks an idn "reviewed — not behind" (re-marking refreshes the
// reason). One audited transaction.
func SetNotBehind(d *sql.DB, idn, reason, by string) error {
	idn, by, reason = strings.TrimSpace(idn), strings.TrimSpace(by), strings.TrimSpace(reason)
	if idn == "" || by == "" {
		return errEmptyField
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM not_behind_acks WHERE idn = ?`, idn); err != nil {
		return err
	}
	ts := compute.NowET().Format("2006-01-02 15:04:05 MST")
	if _, err := tx.Exec(
		`INSERT INTO not_behind_acks (idn, reason, acked_by, created_at) VALUES (?, ?, ?, ?)`,
		idn, nz(reason), by, ts); err != nil {
		return err
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: "not_behind_add", Table: "not_behind_acks", RowID: idn, NewValue: reason,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// ClearNotBehind removes a not-behind ack. Clearing an idn without one is a
// no-op (no audit row).
func ClearNotBehind(d *sql.DB, idn, by string) error {
	idn, by = strings.TrimSpace(idn), strings.TrimSpace(by)
	if idn == "" || by == "" {
		return errEmptyField
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM not_behind_acks WHERE idn = ?`, idn)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return tx.Commit()
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: "not_behind_remove", Table: "not_behind_acks", RowID: idn,
	}); err != nil {
		return err
	}
	return tx.Commit()
}
