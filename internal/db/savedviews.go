// savedviews.go — per-user saved roster views ("saved searches"): a named,
// sanitized filter query string an officer can re-apply with one click. Uses
// the saved_searches table from migration 001 (mirrored in ensureSchemaSQL),
// keyed by user — not idn, so it is NOT in extensionTablesByIDN. Every write
// audited like the rest of the extension layer.
package db

import (
	"database/sql"
	"strings"

	"pretrial-knoxc/internal/models"
)

// ListSavedViews returns the user's saved views for one page, alphabetical.
// Tolerant of a DB without the table (returns nil).
func ListSavedViews(d *sql.DB, user, page string) ([]models.SavedView, error) {
	if !tableExists(d, "saved_searches") {
		return nil, nil
	}
	rows, err := d.Query(
		`SELECT search_id, name, spec, IFNULL(created_at,'')
		   FROM saved_searches
		  WHERE user_id = ? AND IFNULL(page_path,'') = ?
		  ORDER BY name COLLATE NOCASE`,
		strings.TrimSpace(user), page)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.SavedView
	for rows.Next() {
		var v models.SavedView
		if err := rows.Scan(&v.ID, &v.Name, &v.Query, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SaveView upserts a view by (user, name, page) — re-saving a name replaces
// its query. One audited transaction.
func SaveView(d *sql.DB, user, name, query, page string) error {
	user, name = strings.TrimSpace(user), strings.TrimSpace(name)
	if user == "" || name == "" {
		return errEmptyField
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`DELETE FROM saved_searches WHERE user_id = ? AND name = ? AND IFNULL(page_path,'') = ?`,
		user, name, page); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO saved_searches (user_id, name, spec, page_path) VALUES (?, ?, ?, ?)`,
		user, name, query, page); err != nil {
		return err
	}
	if err := WriteAudit(tx, AuditEvent{
		User: user, Action: "view_save", Table: "saved_searches",
		NewValue: clip(name + " → " + query),
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteSavedView removes one of the CALLER's views — the user scope means
// nobody can delete someone else's saved view by guessing an id.
func DeleteSavedView(d *sql.DB, id int64, user string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var name string
	_ = tx.QueryRow(`SELECT name FROM saved_searches WHERE search_id = ? AND user_id = ?`,
		id, strings.TrimSpace(user)).Scan(&name)
	res, err := tx.Exec(`DELETE FROM saved_searches WHERE search_id = ? AND user_id = ?`,
		id, strings.TrimSpace(user))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // not theirs (or already gone) — nothing to do, nothing to audit
	}
	if err := WriteAudit(tx, AuditEvent{
		User: user, Action: "view_delete", Table: "saved_searches", OldValue: clip(name),
	}); err != nil {
		return err
	}
	return tx.Commit()
}
