// schedcheckins.go — scheduled (booked) future check-ins: migration 001 §12's
// "calendar of upcoming check-ins" table, finally in use. An officer books an
// appointment on the record; it shows in the record's Check-ins tab and on the
// console dashboard's Today's Schedule the day it falls due. Display-only with
// respect to the compliance math — the real check-in is still logged
// separately, and the record marks a booking ✓ done when an actual check-in
// exists on that day (derived at read time; completed_check_in_id stays
// unused). Same shape as the other extension tables: app-owned
// (importer-proof), every mutation audited, purged on whole-person delete
// (scheduled_check_ins is in extensionTablesByIDN).
package db

import (
	"database/sql"
	"strings"

	"pretrial-knoxc/internal/models"
)

const schedSelect = `SELECT sched_id, idn, scheduled_for, IFNULL(check_in_type,''),
       IFNULL(officer,''), IFNULL(created_by,''), IFNULL(created_at,'')
  FROM scheduled_check_ins`

func scanScheds(rows *sql.Rows) ([]models.ScheduledCheckIn, error) {
	defer rows.Close()
	var out []models.ScheduledCheckIn
	for rows.Next() {
		var s models.ScheduledCheckIn
		if err := rows.Scan(&s.ID, &s.IDN, &s.For, &s.Type, &s.Officer, &s.CreatedBy, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListScheduledCheckIns returns one client's booked check-ins, soonest first
// (scheduled_for is ISO yyyy-mm-dd from the date input, so TEXT order is
// chronological; odd hand-entered formats just sort lexically and are parsed
// leniently at display time).
func ListScheduledCheckIns(d *sql.DB, idn string) ([]models.ScheduledCheckIn, error) {
	rows, err := d.Query(schedSelect+` WHERE idn = ? ORDER BY scheduled_for, sched_id`, idn)
	if err != nil {
		return nil, err
	}
	return scanScheds(rows)
}

// ListAllScheduledCheckIns returns every booked check-in across all defendants,
// soonest first — the dashboard's Today's Schedule feed. Tolerant of a DB
// without the table (returns nil), like the other ListAll* readers.
func ListAllScheduledCheckIns(d *sql.DB) ([]models.ScheduledCheckIn, error) {
	if !tableExists(d, "scheduled_check_ins") {
		return nil, nil
	}
	rows, err := d.Query(schedSelect + ` ORDER BY scheduled_for, sched_id`)
	if err != nil {
		return nil, err
	}
	return scanScheds(rows)
}

// AddScheduledCheckIn books a future check-in (any allowed officer; audited).
func AddScheduledCheckIn(d *sql.DB, idn, scheduledFor, checkInType, officer, by string) error {
	idn, scheduledFor = strings.TrimSpace(idn), strings.TrimSpace(scheduledFor)
	if idn == "" || scheduledFor == "" {
		return errEmptyField
	}
	return txAddWithAudit(d,
		AuditEvent{User: by, Action: "sched_add", Table: "scheduled_check_ins", RowID: idn, NewValue: scheduledFor},
		`INSERT INTO scheduled_check_ins (idn, scheduled_for, check_in_type, officer, created_by)
		 VALUES (?, ?, ?, ?, ?)`,
		idn, scheduledFor, nz(checkInType), nz(officer), nz(by))
}

// DeleteScheduledCheckIn cancels a booking (audited).
func DeleteScheduledCheckIn(d *sql.DB, id int64, by string) error {
	return txDeleteByID(d, "scheduled_check_ins", "sched_id", id, by, "sched_delete")
}
