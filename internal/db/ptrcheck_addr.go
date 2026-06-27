// ptrcheck_addr.go — "addressed" marks for the PTR-export-check page (problem
// report #9). An officer ticks a row once a discrepancy has been looked at and
// handled; the mark is per-IDN, app-owned, and audited. Tolerant of a DB without
// the table so read paths stay usable on older snapshots.
package db

import (
	"database/sql"
	"strings"

	"pretrial-knoxc/internal/compute"
)

// LoadPtrCheckAddressed returns the set of idns marked addressed on the PTR-check
// page.
func LoadPtrCheckAddressed(d *sql.DB) (map[string]bool, error) {
	out := map[string]bool{}
	if !tableExists(d, "ptr_check_addressed") {
		return out, nil
	}
	rows, err := d.Query(`SELECT idn FROM ptr_check_addressed`)
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

// SetPtrCheckAddressed marks an idn addressed (idempotent). One audited tx.
func SetPtrCheckAddressed(d *sql.DB, idn, by string) error {
	idn, by = strings.TrimSpace(idn), strings.TrimSpace(by)
	if idn == "" {
		return errEmptyField
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Already addressed → no-op (no duplicate audit row on a double-click).
	var one int
	if tx.QueryRow(`SELECT 1 FROM ptr_check_addressed WHERE idn = ?`, idn).Scan(&one) == nil {
		return tx.Commit()
	}
	ts := compute.NowET().Format("2006-01-02 15:04:05 MST")
	if _, err := tx.Exec(
		`INSERT INTO ptr_check_addressed (idn, addressed_by, created_at) VALUES (?, ?, ?)`,
		idn, nz(by), ts); err != nil {
		return err
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: "ptr_check_addressed", Table: "ptr_check_addressed", RowID: idn,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// ClearPtrCheckAddressed removes an addressed mark. Clearing an absent mark is a
// no-op (no audit row).
func ClearPtrCheckAddressed(d *sql.DB, idn, by string) error {
	idn, by = strings.TrimSpace(idn), strings.TrimSpace(by)
	if idn == "" {
		return errEmptyField
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM ptr_check_addressed WHERE idn = ?`, idn)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return tx.Commit()
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: "ptr_check_unaddressed", Table: "ptr_check_addressed", RowID: idn,
	}); err != nil {
		return err
	}
	return tx.Commit()
}
