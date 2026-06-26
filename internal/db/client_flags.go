// client_flags.go is the data layer for manual client alerts (migration 013): a
// prominent "pay attention to this person" marker an officer raises on a client
// — a safety/absconding risk, do-not-release, etc. — that rides as a banner on
// the record and a chip on the roster until another officer clears it.
// App-owned and audited (never touches raw_*); a clear is a soft deactivate so
// the history survives in the audit trail.
package db

import (
	"database/sql"
	"strconv"
	"strings"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/models"
)

// AddClientFlag raises an alert on a client. Severity is normalized to
// "red" (urgent) or "amber" (caution); anything else defaults to red.
func AddClientFlag(d *sql.DB, idn, severity, reason, by string) error {
	idn = strings.TrimSpace(idn)
	if idn == "" {
		return errEmptyField
	}
	severity = strings.ToLower(strings.TrimSpace(severity))
	if severity != "amber" {
		severity = "red"
	}
	return txAddWithAudit(d,
		AuditEvent{User: by, Action: "client_flag_add", Table: "client_flags", RowID: idn, NewValue: severity + ": " + clip(reason)},
		`INSERT INTO client_flags (idn, severity, reason, created_by) VALUES (?, ?, ?, ?)`,
		idn, severity, nz(reason), nz(by))
}

// ClearClientFlag soft-clears one active flag (stamps cleared_by/at, active=0).
func ClearClientFlag(d *sql.DB, id int64, by string) error {
	ts := compute.NowET().Format("2006-01-02 15:04:05 MST")
	return txAddWithAudit(d,
		AuditEvent{User: by, Action: "client_flag_clear", Table: "client_flags", RowID: strconv.FormatInt(id, 10)},
		`UPDATE client_flags SET active = 0, cleared_by = ?, cleared_at = ? WHERE flag_id = ? AND active = 1`,
		nz(by), ts, id)
}

// ListActiveFlags returns a client's active flags, urgent (red) first then newest.
func ListActiveFlags(d *sql.DB, idn string) ([]models.ClientFlag, error) {
	rows, err := d.Query(`
		SELECT flag_id, idn, IFNULL(severity,'red'), IFNULL(reason,''), IFNULL(created_by,''), IFNULL(created_at,'')
		  FROM client_flags
		 WHERE idn = ? AND active = 1
		 ORDER BY CASE severity WHEN 'red' THEN 0 ELSE 1 END, flag_id DESC`, strings.TrimSpace(idn))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ClientFlag
	for rows.Next() {
		var f models.ClientFlag
		if err := rows.Scan(&f.ID, &f.IDN, &f.Severity, &f.Reason, &f.CreatedBy, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ActiveFlagSeverity returns idn → the client's highest active flag severity
// ("red" outranks "amber"), for decorating the roster in one query. Tolerant of
// a pre-013 DB (returns an empty map).
func ActiveFlagSeverity(d *sql.DB) map[string]string {
	out := map[string]string{}
	rows, err := d.Query(`SELECT idn, severity FROM client_flags WHERE active = 1`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var idn, sev string
		if rows.Scan(&idn, &sev) != nil {
			continue
		}
		if out[idn] == "red" { // already at the top severity
			continue
		}
		if sev == "" {
			sev = "red"
		}
		out[idn] = sev
	}
	return out
}
