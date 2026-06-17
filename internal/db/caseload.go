// caseload.go implements A–Z caseload assignment: supervisors divide the roster
// by the first letter of the defendant's last name, each letter owned by exactly
// one officer. A new referral left on "Auto — by last name" is assigned to the
// owner of its last-name initial (see the AddDefendant handler), so the officer no
// longer has to be typed in for every intake. State lives in caseload_letters
// (migration 009); every save is audited.
package db

import (
	"database/sql"
	"strconv"
	"strings"
	"unicode"

	"pretrial-knoxc/internal/compute"
)

// CaseloadLetter is one A–Z slot with its assigned officer (blank when unassigned).
type CaseloadLetter struct {
	Letter  string
	Officer string
}

// Letters is the fixed A–Z column order for the admin caseload grid.
var Letters = func() []string {
	out := make([]string, 0, 26)
	for c := 'A'; c <= 'Z'; c++ {
		out = append(out, string(c))
	}
	return out
}()

// LoadCaseloadLetters returns the letter→officer map (letters upper-cased).
// Tolerant of a DB that predates migration 009 (returns an empty map).
func LoadCaseloadLetters(d *sql.DB) (map[string]string, error) {
	out := map[string]string{}
	if !tableExists(d, "caseload_letters") {
		return out, nil
	}
	rows, err := d.Query("SELECT letter, officer FROM caseload_letters")
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var letter, officer string
		if err := rows.Scan(&letter, &officer); err != nil {
			return out, err
		}
		letter = strings.ToUpper(strings.TrimSpace(letter))
		officer = strings.TrimSpace(officer)
		if letter == "" || officer == "" {
			continue
		}
		out[letter] = officer
	}
	return out, rows.Err()
}

// lastNameInitial returns the upper-case A–Z initial of the defendant's last name,
// or "" when there isn't one. Names are stored "LAST, FIRST" (SharePoint), so the
// last name is the text before the first comma; with no comma we fall back to the
// first whitespace-separated token. A non-alphabetic initial yields "".
func lastNameInitial(fullName string) string {
	name := strings.TrimSpace(fullName)
	if name == "" {
		return ""
	}
	if i := strings.IndexByte(name, ','); i >= 0 {
		name = name[:i]
	} else if fields := strings.Fields(name); len(fields) > 0 {
		name = fields[0]
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	c := unicode.ToUpper([]rune(name)[0])
	if c < 'A' || c > 'Z' {
		return ""
	}
	return string(c)
}

// OfficerForLastName resolves the supervising officer for a defendant name via the
// A–Z caseload map. Returns "" when the initial is unmapped or non-alphabetic, so
// the caller can leave the officer blank rather than guess.
func OfficerForLastName(d *sql.DB, fullName string) string {
	letter := lastNameInitial(fullName)
	if letter == "" {
		return ""
	}
	letters, err := LoadCaseloadLetters(d)
	if err != nil {
		return ""
	}
	return letters[letter]
}

// SetCaseloadAssignments replaces the whole caseload map with the desired state
// (the admin form submits every owned letter each save). Letters are upper-cased
// and validated A–Z; empty officers drop the letter. One audit row records the save.
func SetCaseloadAssignments(d *sql.DB, assignments map[string]string, by string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM caseload_letters"); err != nil {
		return err
	}
	now := compute.NowET().Format("2006-01-02 15:04:05 MST")
	n := 0
	for letter, officer := range assignments {
		letter = strings.ToUpper(strings.TrimSpace(letter))
		officer = strings.TrimSpace(officer)
		if officer == "" || len(letter) != 1 || letter[0] < 'A' || letter[0] > 'Z' {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO caseload_letters (letter, officer, author, updated_at) VALUES (?,?,?,?)`,
			letter, officer, nz(by), now,
		); err != nil {
			return err
		}
		n++
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: "caseload_set", Table: "caseload_letters",
		NewValue: clip("letters assigned: " + strconv.Itoa(n)),
	}); err != nil {
		return err
	}
	return tx.Commit()
}
