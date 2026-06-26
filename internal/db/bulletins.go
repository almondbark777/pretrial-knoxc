// bulletins.go is the data layer for the office-wide notice board shown on the
// check-in page (migration 013): a persistent announcement every officer sees,
// unlike the ephemeral 7-day group chat. App-owned and audited; high-priority
// and pinned posts sort to the top. A removal is a soft deactivate so the audit
// trail survives.
package db

import (
	"database/sql"
	"strconv"
	"strings"

	"pretrial-knoxc/internal/models"
)

// AddBulletin posts a notice. Priority is normalized to "high" or "normal".
func AddBulletin(d *sql.DB, title, body, priority string, pinned bool, by string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errEmptyField
	}
	priority = strings.ToLower(strings.TrimSpace(priority))
	if priority != "high" {
		priority = "normal"
	}
	pin := 0
	if pinned {
		pin = 1
	}
	return txAddWithAudit(d,
		AuditEvent{User: by, Action: "bulletin_add", Table: "bulletins", NewValue: clip(title)},
		`INSERT INTO bulletins (title, body, priority, pinned, created_by) VALUES (?, ?, ?, ?, ?)`,
		title, nz(body), priority, pin, nz(by))
}

// RemoveBulletin soft-removes one post (active=0, audited).
func RemoveBulletin(d *sql.DB, id int64, by string) error {
	return txAddWithAudit(d,
		AuditEvent{User: by, Action: "bulletin_remove", Table: "bulletins", RowID: strconv.FormatInt(id, 10)},
		`UPDATE bulletins SET active = 0 WHERE bulletin_id = ?`, id)
}

// ListBulletins returns active posts: pinned first, then high-priority, then
// newest. Tolerant of a pre-013 DB (returns nil).
func ListBulletins(d *sql.DB) ([]models.Bulletin, error) {
	rows, err := d.Query(`
		SELECT bulletin_id, title, IFNULL(body,''), IFNULL(priority,'normal'), IFNULL(pinned,0),
		       IFNULL(created_by,''), IFNULL(created_at,'')
		  FROM bulletins
		 WHERE active = 1
		 ORDER BY pinned DESC, CASE priority WHEN 'high' THEN 0 ELSE 1 END, bulletin_id DESC`)
	if err != nil {
		return nil, nil // pre-migration DB: no board yet
	}
	defer rows.Close()
	var out []models.Bulletin
	for rows.Next() {
		var b models.Bulletin
		var pinned int
		if err := rows.Scan(&b.ID, &b.Title, &b.Body, &b.Priority, &pinned, &b.CreatedBy, &b.CreatedAt); err != nil {
			return nil, err
		}
		b.Pinned = pinned != 0
		out = append(out, b)
	}
	return out, rows.Err()
}
