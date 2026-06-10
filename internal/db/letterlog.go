// letterlog.go — per-client letter-generation history (migration 007).
//
// The past-due EM-fees report records every memo the site generates — single
// download or batch zip — so officers can see when each client last had a
// letter and choose who belongs in the next print run. App-owned, purged with
// the person on a whole-person delete (extensionTablesByIDN), audited.
package db

import (
	"database/sql"
	"fmt"
	"strings"

	"pretrial-knoxc/internal/compute"
)

// LetterRef identifies one generated letter: the client, the case the memo
// names, and a short human detail ("behind $640.00 · open").
type LetterRef struct {
	IDN    string
	Case   string
	Detail string
}

// LetterStamp is the most recent generation for one client.
type LetterStamp struct {
	At string // created_at, "2006-01-02 15:04:05 MST"
	By string
}

// LogLetters records one generation event: a letter_log row per memo plus a
// single audit row for the event (one batch = one audit entry, not N), all in
// one transaction.
func LogLetters(d *sql.DB, by, letterType string, refs []LetterRef) error {
	if len(refs) == 0 {
		return nil
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := compute.NowET().Format("2006-01-02 15:04:05 MST")
	for _, ref := range refs {
		idn := strings.TrimSpace(ref.IDN)
		if idn == "" {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO letter_log (idn, case_number, letter_type, detail, generated_by, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			idn, nz(ref.Case), letterType, nz(ref.Detail), nz(by), ts); err != nil {
			return err
		}
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: "letters_generated", Table: "letter_log",
		NewValue: fmt.Sprintf("%d %s memo(s)", len(refs), letterType),
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// LastLetters returns each client's most recent letter generation for the
// given type. Tolerant of a pre-migration DB (missing table reads as empty) —
// callers just show "—".
func LastLetters(d *sql.DB, letterType string) map[string]LetterStamp {
	out := map[string]LetterStamp{}
	rows, err := d.Query(
		`SELECT idn, IFNULL(generated_by,''), created_at FROM letter_log
		  WHERE letter_type = ? ORDER BY created_at, letter_id`, letterType)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var idn, by, at string
		if err := rows.Scan(&idn, &by, &at); err != nil {
			continue
		}
		out[idn] = LetterStamp{At: at, By: by} // ordered ascending — last write wins
	}
	return out
}
