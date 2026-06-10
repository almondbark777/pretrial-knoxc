// pins.go — per-user pinned (starred) defendants: the Go port of the old
// Python qx.list_pinned / is_pinned / toggle_pin trio, surfacing as the
// console record's Pin-client action and the dashboard's pinned quick list.
// Same shape as the other extension tables: writes go to the app-owned
// pinned_defendants table only (created by migration 001, mirrored in
// ensureSchemaSQL), every mutation writes one audit_log breadcrumb in the
// same transaction, and a whole-person delete purges the rows
// (pinned_defendants is in extensionTablesByIDN).
package db

import (
	"database/sql"
	"strings"
)

// PinnedIDNs returns the IDNs the user has pinned, newest pin first. Tolerant
// of a DB without the table (returns nil) so read paths stay usable on a
// snapshot that predates migration 001.
func PinnedIDNs(d *sql.DB, user string) ([]string, error) {
	if !tableExists(d, "pinned_defendants") {
		return nil, nil
	}
	rows, err := d.Query(
		`SELECT idn FROM pinned_defendants WHERE user_id = ? ORDER BY pin_id DESC`,
		strings.TrimSpace(user))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var idn string
		if err := rows.Scan(&idn); err != nil {
			return nil, err
		}
		out = append(out, idn)
	}
	return out, rows.Err()
}

// IsPinned reports whether the user has pinned this defendant.
func IsPinned(d *sql.DB, user, idn string) bool {
	if !tableExists(d, "pinned_defendants") {
		return false
	}
	var one int
	err := d.QueryRow(
		`SELECT 1 FROM pinned_defendants WHERE user_id = ? AND idn = ?`,
		strings.TrimSpace(user), strings.TrimSpace(idn)).Scan(&one)
	return err == nil
}

// TogglePin pins an unpinned defendant and unpins a pinned one, returning the
// new state. One audited transaction either way.
func TogglePin(d *sql.DB, user, idn string) (pinned bool, err error) {
	user, idn = strings.TrimSpace(user), strings.TrimSpace(idn)
	if user == "" || idn == "" {
		return false, errEmptyField
	}
	tx, err := d.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var one int
	was := tx.QueryRow(
		`SELECT 1 FROM pinned_defendants WHERE user_id = ? AND idn = ?`, user, idn).Scan(&one) == nil
	if was {
		if _, err := tx.Exec(
			`DELETE FROM pinned_defendants WHERE user_id = ? AND idn = ?`, user, idn); err != nil {
			return false, err
		}
	} else {
		if _, err := tx.Exec(
			`INSERT INTO pinned_defendants (user_id, idn) VALUES (?, ?)`, user, idn); err != nil {
			return false, err
		}
	}
	action := "pin_add"
	if was {
		action = "pin_remove"
	}
	if err := WriteAudit(tx, AuditEvent{
		User: user, Action: action, Table: "pinned_defendants", RowID: idn,
	}); err != nil {
		return false, err
	}
	return !was, tx.Commit()
}
