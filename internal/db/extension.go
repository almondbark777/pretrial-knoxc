// extension.go is the per-defendant data-entry CRUD: notes, tags, court dates,
// reminders, and violations — all written to extension tables only (never
// raw_*). Every mutating call writes one audit_log breadcrumb in the same
// transaction. Reads back the active tombstones/overrides for the admin views.
package db

import (
	"database/sql"
	"strconv"
	"strings"

	"pretrial-knoxc/internal/models"
)

// scanIDN binds idn as the column value. Extension idn columns have INTEGER
// affinity; SQLite coerces the numeric IDN string on insert and comparison.

// ── Notes ─────────────────────────────────────────────────────────────────

func ListNotes(d *sql.DB, idn string) ([]models.Note, error) {
	rows, err := d.Query(
		`SELECT note_id, idn, IFNULL(author,''), body, IFNULL(created_at,'')
		   FROM defendant_notes WHERE idn = ? ORDER BY created_at DESC, note_id DESC`, idn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Note
	for rows.Next() {
		var n models.Note
		if err := rows.Scan(&n.ID, &n.IDN, &n.Author, &n.Body, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func AddNote(d *sql.DB, idn, body, author string) error {
	if strings.TrimSpace(idn) == "" || strings.TrimSpace(body) == "" {
		return errEmptyField
	}
	return txAddWithAudit(d, AuditEvent{User: author, Action: "note_add", Table: "defendant_notes", RowID: idn, NewValue: clip(body)},
		`INSERT INTO defendant_notes (idn, author, body) VALUES (?, ?, ?)`, idn, nz(author), body)
}

func DeleteNote(d *sql.DB, id int64, by string) error {
	return txDeleteByID(d, "defendant_notes", "note_id", id, by, "note_delete")
}

// ── Tags ──────────────────────────────────────────────────────────────────

func ListTags(d *sql.DB, idn string) ([]models.Tag, error) {
	rows, err := d.Query(
		`SELECT tag_id, idn, label, IFNULL(author,''), IFNULL(created_at,'')
		   FROM defendant_tags WHERE idn = ? ORDER BY label`, idn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Tag
	for rows.Next() {
		var t models.Tag
		if err := rows.Scan(&t.ID, &t.IDN, &t.Label, &t.Author, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func AddTag(d *sql.DB, idn, label, author string) error {
	if strings.TrimSpace(idn) == "" || strings.TrimSpace(label) == "" {
		return errEmptyField
	}
	return txAddWithAudit(d, AuditEvent{User: author, Action: "tag_add", Table: "defendant_tags", RowID: idn, NewValue: clip(label)},
		`INSERT INTO defendant_tags (idn, label, author) VALUES (?, ?, ?)`, idn, label, nz(author))
}

func DeleteTag(d *sql.DB, id int64, by string) error {
	return txDeleteByID(d, "defendant_tags", "tag_id", id, by, "tag_delete")
}

// ── Court dates ─────────────────────────────────────────────────────────────

func ListCourtDates(d *sql.DB, idn string) ([]models.CourtDate, error) {
	rows, err := d.Query(
		`SELECT court_date_id, idn, court_date, IFNULL(court,''), IFNULL(notes,''), IFNULL(author,''), IFNULL(created_at,'')
		   FROM court_dates WHERE idn = ? ORDER BY court_date`, idn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.CourtDate
	for rows.Next() {
		var c models.CourtDate
		if err := rows.Scan(&c.ID, &c.IDN, &c.CourtDate, &c.Court, &c.Notes, &c.Author, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func AddCourtDate(d *sql.DB, idn, courtDate, court, notes, author string) error {
	if strings.TrimSpace(idn) == "" || strings.TrimSpace(courtDate) == "" {
		return errEmptyField
	}
	return txAddWithAudit(d, AuditEvent{User: author, Action: "courtdate_add", Table: "court_dates", RowID: idn, NewValue: courtDate},
		`INSERT INTO court_dates (idn, court_date, court, notes, author) VALUES (?, ?, ?, ?, ?)`,
		idn, courtDate, nz(court), nz(notes), nz(author))
}

func DeleteCourtDate(d *sql.DB, id int64, by string) error {
	return txDeleteByID(d, "court_dates", "court_date_id", id, by, "courtdate_delete")
}

// ── Reminders ─────────────────────────────────────────────────────────────

func ListReminders(d *sql.DB, idn string) ([]models.Reminder, error) {
	rows, err := d.Query(
		`SELECT reminder_id, IFNULL(idn,''), body, IFNULL(due_date,''), IFNULL(assigned_to,''),
		        IFNULL(created_by,''), completed, IFNULL(created_at,'')
		   FROM reminders WHERE idn = ? ORDER BY completed, due_date`, idn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Reminder
	for rows.Next() {
		var r models.Reminder
		var done int
		if err := rows.Scan(&r.ID, &r.IDN, &r.Body, &r.DueDate, &r.AssignedTo, &r.CreatedBy, &done, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.Completed = done != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func AddReminder(d *sql.DB, idn, body, dueDate, assignedTo, by string) error {
	if strings.TrimSpace(idn) == "" || strings.TrimSpace(body) == "" {
		return errEmptyField
	}
	return txAddWithAudit(d, AuditEvent{User: by, Action: "reminder_add", Table: "reminders", RowID: idn, NewValue: clip(body)},
		`INSERT INTO reminders (idn, body, due_date, assigned_to, created_by) VALUES (?, ?, ?, ?, ?)`,
		idn, body, nz(dueDate), nz(assignedTo), nz(by))
}

func DeleteReminder(d *sql.DB, id int64, by string) error {
	return txDeleteByID(d, "reminders", "reminder_id", id, by, "reminder_delete")
}

// ── Violations ──────────────────────────────────────────────────────────────

func ListViolations(d *sql.DB, idn string) ([]models.Violation, error) {
	rows, err := d.Query(
		`SELECT violation_id, idn, violation_date, IFNULL(category,''), IFNULL(severity,''),
		        IFNULL(description,''), IFNULL(action_taken,''), IFNULL(officer,''), IFNULL(created_at,'')
		   FROM violations WHERE idn = ? ORDER BY violation_date DESC, violation_id DESC`, idn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Violation
	for rows.Next() {
		var v models.Violation
		if err := rows.Scan(&v.ID, &v.IDN, &v.ViolationDate, &v.Category, &v.Severity,
			&v.Description, &v.ActionTaken, &v.Officer, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func AddViolation(d *sql.DB, idn, date, category, severity, description, actionTaken, officer string) error {
	if strings.TrimSpace(idn) == "" || strings.TrimSpace(date) == "" {
		return errEmptyField
	}
	return txAddWithAudit(d, AuditEvent{User: officer, Action: "violation_add", Table: "violations", RowID: idn, NewValue: category},
		`INSERT INTO violations (idn, violation_date, category, severity, description, action_taken, officer)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		idn, date, nz(category), nz(severity), nz(description), nz(actionTaken), nz(officer))
}

func DeleteViolation(d *sql.DB, id int64, by string) error {
	return txDeleteByID(d, "violations", "violation_id", id, by, "violation_delete")
}

// ── Admin views: tombstones + overrides ──────────────────────────────────────

// ListTombstones returns every active tombstone, newest first, resolving the
// person's name from raw_blue_book when the row is still present (it is, unless
// physically deleted under IMPORTER_RETIRED).
func ListTombstones(d *sql.DB) ([]models.Tombstone, error) {
	rows, err := d.Query(
		`SELECT t.idn, IFNULL(t.case_number,''), IFNULL(t.deleted_by,''), IFNULL(t.deleted_at,''),
		        IFNULL(t.reason,''),
		        IFNULL((SELECT bb.defendant FROM raw_blue_book bb WHERE bb.idn = t.idn LIMIT 1), '')
		   FROM deleted_idns t ORDER BY t.deleted_at DESC, t.tombstone_id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Tombstone
	for rows.Next() {
		var t models.Tombstone
		if err := rows.Scan(&t.IDN, &t.CaseNumber, &t.DeletedBy, &t.DeletedAt, &t.Reason, &t.Name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListOverrides returns a defendant's active overrides for the profile panel.
func ListOverrides(d *sql.DB, idn string) ([]models.Override, error) {
	rows, err := d.Query(
		`SELECT field, value, IFNULL(author,''), IFNULL(updated_at,'')
		   FROM overrides WHERE idn = ? ORDER BY field`, idn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := map[string]string{}
	for _, f := range OverridableFields() {
		labels[f.Key] = f.Label
	}
	var out []models.Override
	for rows.Next() {
		var o models.Override
		if err := rows.Scan(&o.Field, &o.Value, &o.Author, &o.UpdatedAt); err != nil {
			return nil, err
		}
		o.Label = labels[o.Field]
		if o.Label == "" {
			o.Label = o.Field
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// LoadExtras gathers all of a defendant's app-owned data for the profile page.
func LoadExtras(d *sql.DB, idn string) (models.DefendantExtras, error) {
	var e models.DefendantExtras
	var err error
	if e.Notes, err = ListNotes(d, idn); err != nil {
		return e, err
	}
	if e.Tags, err = ListTags(d, idn); err != nil {
		return e, err
	}
	if e.CourtDates, err = ListCourtDates(d, idn); err != nil {
		return e, err
	}
	if e.Reminders, err = ListReminders(d, idn); err != nil {
		return e, err
	}
	if e.Violations, err = ListViolations(d, idn); err != nil {
		return e, err
	}
	if e.Overrides, err = ListOverrides(d, idn); err != nil {
		return e, err
	}
	return e, nil
}

// ── shared write helpers (insert/delete + audit in one transaction) ──────────

// txAddWithAudit runs one INSERT and one audit row atomically.
func txAddWithAudit(d *sql.DB, ev AuditEvent, query string, args ...any) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(query, args...); err != nil {
		return err
	}
	if err := WriteAudit(tx, ev); err != nil {
		return err
	}
	return tx.Commit()
}

// txDeleteByID deletes one extension row by its primary key, capturing the
// owning idn first so the audit breadcrumb points at the defendant.
func txDeleteByID(d *sql.DB, table, pkCol string, id int64, by, action string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var idn sql.NullString
	_ = tx.QueryRow("SELECT idn FROM "+table+" WHERE "+pkCol+" = ?", id).Scan(&idn)
	if _, err := tx.Exec("DELETE FROM "+table+" WHERE "+pkCol+" = ?", id); err != nil {
		return err
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: action, Table: table, RowID: idn.String,
		OldValue: pkCol + "=" + strconv.FormatInt(id, 10),
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// clip trims an audit value to a reasonable length.
func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

const errEmptyField = adminErr("required field missing")
