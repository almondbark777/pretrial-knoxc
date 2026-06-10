// drugscreens.go is the drug-screen log CRUD (officer-level, audited) — the
// "drug-screen logging (table + CRUD)" roadmap item carried over from the old
// Python app. Same shape as the other extension tables: writes go to the
// app-owned drug_screens table only (never raw_*), every mutation writes one
// audit_log breadcrumb in the same transaction, and a whole-person delete
// purges the rows (drug_screens is in extensionTablesByIDN).
//
// Canonical DDL: db/migrations/005_drugscreens_sqlite.sql, mirrored in
// ensureSchemaSQL (admin.go) so the server self-provisions at startup.
package db

import (
	"database/sql"
	"strings"

	"pretrial-knoxc/internal/models"
)

// ListDrugScreens returns one defendant's drug screens, newest first.
func ListDrugScreens(d *sql.DB, idn string) ([]models.DrugScreen, error) {
	rows, err := d.Query(
		`SELECT screen_id, idn, screen_date, IFNULL(test_type,''), IFNULL(result,''),
		        IFNULL(substances,''), IFNULL(notes,''), IFNULL(officer,''), IFNULL(created_at,'')
		   FROM drug_screens WHERE idn = ? ORDER BY screen_date DESC, screen_id DESC`, idn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.DrugScreen
	for rows.Next() {
		var s models.DrugScreen
		if err := rows.Scan(&s.ID, &s.IDN, &s.ScreenDate, &s.TestType, &s.Result,
			&s.Substances, &s.Notes, &s.Officer, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// AddDrugScreen records one screen. idn and date are required; the audit value
// captures result + test type so the breadcrumb is meaningful on its own.
func AddDrugScreen(d *sql.DB, idn, date, testType, result, substances, notes, officer string) error {
	if strings.TrimSpace(idn) == "" || strings.TrimSpace(date) == "" {
		return errEmptyField
	}
	return txAddWithAudit(d, AuditEvent{User: officer, Action: "drugscreen_add", Table: "drug_screens",
		RowID: idn, NewValue: clip(strings.TrimSpace(result + " " + testType))},
		`INSERT INTO drug_screens (idn, screen_date, test_type, result, substances, notes, officer)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		idn, date, nz(testType), nz(result), nz(substances), nz(notes), nz(officer))
}

// DeleteDrugScreen removes one screen row (officer-level; audited).
func DeleteDrugScreen(d *sql.DB, id int64, by string) error {
	return txDeleteByID(d, "drug_screens", "screen_id", id, by, "drugscreen_delete")
}

// ListAllDrugScreens returns every recorded screen across all defendants,
// newest first. Tolerant of a DB without the table (returns nil) so read paths
// stay usable on a snapshot that predates the feature.
func ListAllDrugScreens(d *sql.DB) ([]models.DrugScreen, error) {
	if !tableExists(d, "drug_screens") {
		return nil, nil
	}
	rows, err := d.Query(
		`SELECT screen_id, idn, screen_date, IFNULL(test_type,''), IFNULL(result,''),
		        IFNULL(substances,''), IFNULL(notes,''), IFNULL(officer,''), IFNULL(created_at,'')
		   FROM drug_screens ORDER BY screen_date DESC, screen_id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.DrugScreen
	for rows.Next() {
		var s models.DrugScreen
		if err := rows.Scan(&s.ID, &s.IDN, &s.ScreenDate, &s.TestType, &s.Result,
			&s.Substances, &s.Notes, &s.Officer, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
