// admin.go is the write/correction side of the data layer (Phase 7). It owns:
//
//   - Tombstones (deleted_idns): a durable, importer-proof delete. BuildClients
//     filters tombstoned idns/cases on every read, so a deleted person/case
//     vanishes from EVERY view and STAYS gone across the Sunday full reload of
//     raw_blue_book (the importer never touches this extension table).
//   - Overrides (overrides): supervisor typo-fixes to imported fields, applied
//     in BuildClients after the raw read.
//   - The audit_log breadcrumb: one row per write action, stamped in ET.
//   - Per-defendant extension CRUD (notes / tags / court dates / reminders /
//     violations), written to extension tables only.
//
// HARD RULE (Brief 5.4): never write to raw_* tables EXCEPT the IMPORTER_RETIRED
// physical-delete path. All other writes go to extension tables.
package db

import (
	"database/sql"
	"strconv"
	"strings"
	"time"

	"pretrial-knoxc/internal/compute"
)

// ── Schema bootstrap ────────────────────────────────────────────────────────

// ensureSchemaSQL mirrors the DDL the Go app writes to: the new 003 tables
// (deleted_idns, overrides) plus the 001 extension tables the CRUD endpoints
// touch. The canonical migration files are db/migrations/001_app_extensions_sqlite.sql
// and db/migrations/003_admin_sqlite.sql; this inlined subset lets the server
// self-provision a fresh DB at startup (Brief: "add a startup CREATE TABLE IF
// NOT EXISTS for the new tables"). All statements are IF NOT EXISTS / idempotent.
const ensureSchemaSQL = `
CREATE TABLE IF NOT EXISTS deleted_idns (
    tombstone_id INTEGER PRIMARY KEY AUTOINCREMENT,
    idn          TEXT NOT NULL,
    case_number  TEXT NULL,
    deleted_by   TEXT NULL,
    deleted_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    reason       TEXT NULL
);
CREATE INDEX IF NOT EXISTS idx_deleted_idns_idn ON deleted_idns(idn);
CREATE UNIQUE INDEX IF NOT EXISTS uq_deleted_idns ON deleted_idns(idn, IFNULL(case_number, ''));

CREATE TABLE IF NOT EXISTS overrides (
    override_id INTEGER PRIMARY KEY AUTOINCREMENT,
    idn         TEXT NOT NULL,
    field       TEXT NOT NULL,
    value       TEXT NOT NULL,
    author      TEXT NULL,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_overrides_idn ON overrides(idn);
CREATE UNIQUE INDEX IF NOT EXISTS uq_overrides ON overrides(idn, field);

CREATE TABLE IF NOT EXISTS audit_log (
    audit_id   INTEGER PRIMARY KEY AUTOINCREMENT,
    ts         TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user_id    TEXT NULL,
    action     TEXT NOT NULL,
    table_name TEXT NOT NULL,
    row_id     TEXT NULL,
    col_name   TEXT NULL,
    old_value  TEXT NULL,
    new_value  TEXT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_ts  ON audit_log(ts);
CREATE INDEX IF NOT EXISTS idx_audit_row ON audit_log(table_name, row_id);

CREATE TABLE IF NOT EXISTS defendant_notes (
    note_id    INTEGER PRIMARY KEY AUTOINCREMENT,
    idn        INTEGER NOT NULL,
    author     TEXT NULL,
    body       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_notes_idn ON defendant_notes(idn);

CREATE TABLE IF NOT EXISTS defendant_tags (
    tag_id     INTEGER PRIMARY KEY AUTOINCREMENT,
    idn        INTEGER NOT NULL,
    label      TEXT NOT NULL,
    author     TEXT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_tags_idn ON defendant_tags(idn);

CREATE TABLE IF NOT EXISTS court_dates (
    court_date_id INTEGER PRIMARY KEY AUTOINCREMENT,
    idn           INTEGER NOT NULL,
    court_date    TEXT NOT NULL,
    court         TEXT NULL,
    notes         TEXT NULL,
    outcome       TEXT NULL,
    next_date     TEXT NULL,
    author        TEXT NULL,
    created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_courtd_idn ON court_dates(idn);

CREATE TABLE IF NOT EXISTS client_dates (
    client_date_id INTEGER PRIMARY KEY AUTOINCREMENT,
    idn            TEXT NOT NULL,
    label          TEXT NOT NULL,
    date_value     TEXT NOT NULL,
    note           TEXT NULL,
    author         TEXT NULL,
    created_at     TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_client_dates_idn ON client_dates(idn);

CREATE TABLE IF NOT EXISTS problem_reports (
    report_id  INTEGER PRIMARY KEY AUTOINCREMENT,
    page       TEXT NULL,
    body       TEXT NOT NULL,
    user_agent TEXT NULL,
    author     TEXT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS violations (
    violation_id   INTEGER PRIMARY KEY AUTOINCREMENT,
    idn            INTEGER NOT NULL,
    violation_date TEXT NOT NULL,
    category       TEXT NULL,
    severity       TEXT NULL,
    description    TEXT NULL,
    action_taken   TEXT NULL,
    officer        TEXT NULL,
    court_notified INTEGER NOT NULL DEFAULT 0,
    da_notified    INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_viol_idn ON violations(idn);

CREATE TABLE IF NOT EXISTS reminders (
    reminder_id  INTEGER PRIMARY KEY AUTOINCREMENT,
    idn          INTEGER NULL,
    body         TEXT NOT NULL,
    due_date     TEXT NULL,
    assigned_to  TEXT NULL,
    created_by   TEXT NULL,
    completed    INTEGER NOT NULL DEFAULT 0,
    completed_at TEXT NULL,
    completed_by TEXT NULL,
    created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_rem_idn ON reminders(idn);

CREATE TABLE IF NOT EXISTS added_defendants (
    add_id              INTEGER PRIMARY KEY AUTOINCREMENT,
    idn                 TEXT NOT NULL,
    defendant           TEXT,
    warrant_case_num    TEXT,
    pretrial_level      TEXT,
    case_status         TEXT,
    supervising_officer TEXT,
    referral_date       TEXT,
    gps                 TEXT,
    gps_type            TEXT,
    charge_type         TEXT,
    bond_amount         TEXT,
    supervision_type    TEXT,
    order_from          TEXT,
    dma                 TEXT,
    birthdate           TEXT,
    closed_date         TEXT,
    day_adjustment      TEXT,
    bond_conditions     TEXT,
    court               TEXT,
    victim              TEXT,
    victim_idn          TEXT,
    victim_2            TEXT,
    victim_2_idn        TEXT,
    victim_3            TEXT,
    victim_3_idn        TEXT,
    victim_time_48      TEXT,
    victim_accept_deny_gps TEXT,
    gps_install_date    TEXT,
    da_emailed          TEXT,
    switched_to         TEXT,
    switched_gps_date   TEXT,
    paid                TEXT,
    court_order         TEXT,
    comments            TEXT,
    received_signed_copy_date TEXT,
    contact_date        TEXT,
    released_to_hilltop_date  TEXT,
    ptr_successfully_completed TEXT,
    author              TEXT,
    created_at          TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_added_def_idn ON added_defendants(idn);
CREATE UNIQUE INDEX IF NOT EXISTS uq_added_def ON added_defendants(idn, IFNULL(warrant_case_num, ''));

CREATE TABLE IF NOT EXISTS added_payments (
    add_id                         INTEGER PRIMARY KEY AUTOINCREMENT,
    idn                            TEXT NOT NULL,
    case_number                    TEXT,
    payment_date                   TEXT,
    payment_amount                 TEXT,
    payment_type                   TEXT,
    officer_that_collected_payment TEXT,
    author                         TEXT,
    created_at                     TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_added_pay_idn ON added_payments(idn);

CREATE TABLE IF NOT EXISTS added_check_ins (
    add_id           INTEGER PRIMARY KEY AUTOINCREMENT,
    idn              TEXT NOT NULL,
    date             TEXT,
    type_of_check_in TEXT,
    note             TEXT,
    author           TEXT,
    created_at       TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_added_ci_idn ON added_check_ins(idn);

CREATE TABLE IF NOT EXISTS drug_screens (
    screen_id   INTEGER PRIMARY KEY AUTOINCREMENT,
    idn         INTEGER NOT NULL,
    screen_date TEXT NOT NULL,
    test_type   TEXT NULL,
    result      TEXT NULL,
    substances  TEXT NULL,
    notes       TEXT NULL,
    officer     TEXT NULL,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_screen_idn ON drug_screens(idn);

CREATE TABLE IF NOT EXISTS custody_periods (
    custody_id  INTEGER PRIMARY KEY AUTOINCREMENT,
    idn         TEXT NOT NULL,
    start_date  TEXT NOT NULL,
    end_date    TEXT NULL,
    note        TEXT NULL,
    author      TEXT NULL,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_custody_idn ON custody_periods(idn);

CREATE TABLE IF NOT EXISTS pinned_defendants (
    pin_id      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     TEXT NOT NULL,
    idn         INTEGER NOT NULL,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_pin UNIQUE(user_id, idn)
);
CREATE INDEX IF NOT EXISTS idx_pin_user ON pinned_defendants(user_id);

CREATE TABLE IF NOT EXISTS saved_searches (
    search_id   INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     TEXT NOT NULL,
    name        TEXT NOT NULL,
    spec        TEXT NOT NULL,
    page_path   TEXT NULL,
    is_pinned   INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_search_user ON saved_searches(user_id);

CREATE TABLE IF NOT EXISTS fee_waivers (
    waiver_id   INTEGER PRIMARY KEY AUTOINCREMENT,
    idn         TEXT NOT NULL,
    reason      TEXT NULL,
    waived_by   TEXT NULL,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_waiver_idn UNIQUE(idn)
);

CREATE TABLE IF NOT EXISTS scheduled_check_ins (
    sched_id        INTEGER PRIMARY KEY AUTOINCREMENT,
    idn             INTEGER NOT NULL,
    scheduled_for   TEXT NOT NULL,
    check_in_type   TEXT NULL,
    officer         TEXT NULL,
    completed_check_in_id INTEGER NULL,
    created_by      TEXT NULL,
    created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sched_idn  ON scheduled_check_ins(idn);
CREATE INDEX IF NOT EXISTS idx_sched_when ON scheduled_check_ins(scheduled_for);

CREATE TABLE IF NOT EXISTS letter_log (
    letter_id    INTEGER PRIMARY KEY AUTOINCREMENT,
    idn          TEXT NOT NULL,
    case_number  TEXT NULL,
    letter_type  TEXT NOT NULL DEFAULT 'em_fees',
    detail       TEXT NULL,
    generated_by TEXT NULL,
    created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_letter_idn ON letter_log(idn);

CREATE TABLE IF NOT EXISTS caseload_letters (
    letter     TEXT PRIMARY KEY,
    officer    TEXT NOT NULL,
    author     TEXT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS app_users (
    email      TEXT PRIMARY KEY,
    role       TEXT NOT NULL DEFAULT 'officer',
    added_by   TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS chat_messages (
    msg_id     INTEGER PRIMARY KEY AUTOINCREMENT,
    author     TEXT NOT NULL,
    body       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_chat_created ON chat_messages(created_at);
`

// EnsureSchema creates the admin + extension tables if they don't exist. Safe to
// run on every startup. modernc's Exec is fed one statement at a time (split on
// ';') so it works regardless of multi-statement support.
func EnsureSchema(d *sql.DB) error {
	for _, stmt := range strings.Split(ensureSchemaSQL, ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := d.Exec(stmt); err != nil {
			return err
		}
	}
	// Additive column migrations for DBs created before these columns existed
	// (CREATE IF NOT EXISTS won't alter an existing table). Court hearing outcome
	// + next date (the FTA-tracking fields).
	if err := addColumnIfMissing(d, "court_dates", "outcome", "TEXT"); err != nil {
		return err
	}
	if err := addColumnIfMissing(d, "court_dates", "next_date", "TEXT"); err != nil {
		return err
	}
	// Per-check-in note (fitment details etc.) on app-entered check-ins.
	if err := addColumnIfMissing(d, "added_check_ins", "note", "TEXT"); err != nil {
		return err
	}
	// Full-referral fields on added_defendants (migration 008) — the console intake
	// wizard now mirrors the SharePoint exports. Names match the raw_blue_book /
	// raw_gps_48_hours columns so they merge by name in every read path.
	for _, col := range addedDefendantReferralCols {
		if err := addColumnIfMissing(d, "added_defendants", col, "TEXT"); err != nil {
			return err
		}
	}
	return nil
}

// addedDefendantReferralCols are the columns added in migration 008 to capture the
// full referral. Kept as a list so EnsureSchema can backfill an existing DB (the
// inline CREATE above already has them for a fresh DB).
var addedDefendantReferralCols = []string{
	"bond_conditions", "court", "victim", "victim_idn", "victim_2", "victim_2_idn",
	"victim_3", "victim_3_idn", "victim_time_48", "victim_accept_deny_gps",
	"gps_install_date", "da_emailed", "switched_to", "switched_gps_date", "paid",
	"court_order", "comments", "received_signed_copy_date", "contact_date",
	"released_to_hilltop_date", "ptr_successfully_completed",
}

// addColumnIfMissing runs an idempotent ALTER TABLE … ADD COLUMN, skipping it
// when the column already exists (SQLite has no ADD COLUMN IF NOT EXISTS).
func addColumnIfMissing(d *sql.DB, table, col, decl string) error {
	rows, err := d.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == col {
			return nil // already present
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = d.Exec("ALTER TABLE " + table + " ADD COLUMN " + col + " " + decl)
	return err
}

// ── Tombstones (read side, consumed by BuildClients) ────────────────────────

// tombstoneSets is the suppression state loaded once per BuildClients: idns
// suppressed entirely, and per-idn sets of suppressed case tokens.
type tombstoneSets struct {
	whole   map[string]bool            // idn -> suppress the whole person
	perCase map[string]map[string]bool // idn -> {caseToken -> suppressed}
}

func (t tombstoneSets) caseSuppressed(idn, warrantCaseNum string) bool {
	cd := t.perCase[idn]
	if cd == nil {
		return false
	}
	for _, tok := range compute.CaseTokens(warrantCaseNum) {
		if cd[tok] {
			return true
		}
	}
	return false
}

// tableExists reports whether a table is present, so the read path tolerates a
// DB that predates the admin migration (the production server runs EnsureSchema
// at startup; this keeps BuildClients usable on any DB regardless).
func tableExists(d *sql.DB, name string) bool {
	var got string
	err := d.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name = ?", name).Scan(&got)
	return err == nil
}

// loadTombstones reads deleted_idns once. A NULL/empty case_number tombstones
// the whole person; a value tombstones each of its case tokens.
func loadTombstones(d *sql.DB) (tombstoneSets, error) {
	ts := tombstoneSets{whole: map[string]bool{}, perCase: map[string]map[string]bool{}}
	if !tableExists(d, "deleted_idns") {
		return ts, nil
	}
	rows, err := d.Query("SELECT idn, case_number FROM deleted_idns")
	if err != nil {
		return ts, err
	}
	defer rows.Close()
	for rows.Next() {
		var idn string
		var cn sql.NullString
		if err := rows.Scan(&idn, &cn); err != nil {
			return ts, err
		}
		idn = strings.TrimSpace(idn)
		if idn == "" {
			continue
		}
		if !cn.Valid || strings.TrimSpace(cn.String) == "" {
			ts.whole[idn] = true
			continue
		}
		set := ts.perCase[idn]
		if set == nil {
			set = map[string]bool{}
			ts.perCase[idn] = set
		}
		for _, tok := range compute.CaseTokens(cn.String) {
			set[tok] = true
		}
	}
	return ts, rows.Err()
}

// ── Overrides (read side, consumed by BuildClients) ─────────────────────────

// loadOverrides reads the overrides table once: idn -> field -> value.
func loadOverrides(d *sql.DB) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	if !tableExists(d, "overrides") {
		return out, nil
	}
	rows, err := d.Query("SELECT idn, field, value FROM overrides")
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var idn, field, value string
		if err := rows.Scan(&idn, &field, &value); err != nil {
			return out, err
		}
		idn = strings.TrimSpace(idn)
		field = strings.TrimSpace(field)
		if idn == "" || !overridableFields[field] {
			continue // ignore unknown/disallowed fields defensively
		}
		m := out[idn]
		if m == nil {
			m = map[string]string{}
			out[idn] = m
		}
		m[field] = value
	}
	return out, rows.Err()
}

// overridableFields is the allow-list of raw_blue_book columns a supervisor may
// override. Keys are the raw column names (spliced straight into the row map in
// BuildClients); the values are human labels for the UI. Restricting to this set
// prevents arbitrary-column injection and keeps overrides to safe, imported,
// per-person fields.
var overridableFields = map[string]bool{
	"pretrial_level":      true,
	"referral_date":       true,
	"case_status":         true,
	"gps_type":            true,
	"closed_date":         true,
	"day_adjustment":      true,
	"supervising_officer": true,
	"defendant":           true,
	// Imported case-info fields — editable from the record's "Edit case info"
	// modal (officer-accessible), spliced onto the blue-book row in BuildClients.
	"charge_type":      true,
	"bond_amount":      true,
	"supervision_type": true,
	"order_from":       true,
	"dma":              true,
	"birthdate":        true,
	// GPS / victim-48-hour detail — editable from the record's "Edit GPS details"
	// modal (officer-accessible), overlaid onto the GPS record in BuildClients.
	"gps_install_date":       true,
	"switched_to":            true,
	"switched_gps_date":      true,
	"da_emailed":             true,
	"court_order":            true,
	"victim_time_48":         true,
	"victim_accept_deny_gps": true,
	"victim":                 true,
	"victim_idn":             true,
	"victim_2":               true,
	"victim_2_idn":           true,
	"victim_3":               true,
	"victim_3_idn":           true,
}

// gpsDetailFields is the subset of overridable fields the GPS-details editor
// writes (officer-accessible). Kept separate from the supervisor "correct field"
// dropdown so that generic-override UI stays focused on imported case fields.
var gpsDetailFields = []string{
	"gps_type", "gps_install_date", "switched_to", "switched_gps_date",
	"da_emailed", "court_order", "victim_time_48", "victim_accept_deny_gps",
	"victim", "victim_idn", "victim_2", "victim_2_idn", "victim_3", "victim_3_idn",
}

// IsGPSDetailField reports whether field is one the GPS-details editor may set.
func IsGPSDetailField(field string) bool {
	field = strings.TrimSpace(field)
	for _, f := range gpsDetailFields {
		if f == field {
			return true
		}
	}
	return false
}

// caseInfoFields is the subset of overridable fields the "Edit case info" editor
// writes (officer-accessible) — the imported case detail plus the single-value
// date fields. Each is spliced onto the blue-book row in BuildClients, so a set
// value shows everywhere, survives the daily import, and is audited & reversible.
var caseInfoFields = []string{
	"defendant", "pretrial_level", "case_status", "supervising_officer",
	"charge_type", "bond_amount", "supervision_type", "order_from", "dma",
	"birthdate", "referral_date", "closed_date",
}

// CaseInfoFields returns the case-info fields the editor writes (stable order).
func CaseInfoFields() []string { return append([]string(nil), caseInfoFields...) }

// FieldOption is one overridable field for the override form's dropdown.
type FieldOption struct {
	Key   string
	Label string
}

// OverridableFields returns the allow-listed fields in a stable display order.
func OverridableFields() []FieldOption {
	return []FieldOption{
		{"pretrial_level", "Pretrial Level"},
		{"referral_date", "Referral Date"},
		{"case_status", "Case Status"},
		{"gps_type", "GPS Type"},
		{"closed_date", "Closed Date"},
		{"day_adjustment", "Day Adjustment"},
		{"supervising_officer", "Supervising Officer"},
		{"defendant", "Name"},
	}
}

// IsOverridable reports whether field is on the override allow-list.
func IsOverridable(field string) bool { return overridableFields[strings.TrimSpace(field)] }

// ReopenedSince returns idn → reopen-time for case_status overrides set to an
// open value on/after cutoff — i.e. cases an officer manually reopened recently.
// The dashboard's new-referrals feed folds these in so a reopened case surfaces
// as fresh activity even though its referral date is old.
func ReopenedSince(d *sql.DB, cutoff time.Time) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	if !tableExists(d, "overrides") {
		return out, nil
	}
	rows, err := d.Query(`SELECT idn, value, IFNULL(updated_at, '') FROM overrides WHERE field = 'case_status'`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var idn, value, updated string
		if err := rows.Scan(&idn, &value, &updated); err != nil {
			return out, err
		}
		idn = strings.TrimSpace(idn)
		if idn == "" || !strings.Contains(strings.ToLower(value), "open") {
			continue // only "Open"-ish statuses count as a reopen ("Closed" has no "open")
		}
		t, ok := parseStamp(updated)
		if !ok || t.Before(cutoff) {
			continue
		}
		out[idn] = t
	}
	return out, rows.Err()
}

// parseStamp reads an overrides.updated_at stamp ("2006-01-02 15:04:05 MST",
// written by SetOverride), falling back to a date-only parse.
func parseStamp(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if len(s) >= 19 {
		if t, err := time.Parse("2006-01-02 15:04:05", s[:19]); err == nil {
			return t, true
		}
	}
	if dt, ok := compute.ParseDay(s); ok {
		return dt, true
	}
	return time.Time{}, false
}

// ── Audit log ───────────────────────────────────────────────────────────────

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// AuditEvent is one audit_log row.
type AuditEvent struct {
	User     string
	Action   string
	Table    string
	RowID    string
	Col      string
	OldValue string
	NewValue string
}

// WriteAudit appends one breadcrumb row, stamped with the current Eastern-time
// instant (per the brief). Accepts a *sql.DB or *sql.Tx so it can join a wider
// transaction.
func WriteAudit(x execer, ev AuditEvent) error {
	ts := compute.NowET().Format("2006-01-02 15:04:05 MST")
	_, err := x.Exec(
		`INSERT INTO audit_log (ts, user_id, action, table_name, row_id, col_name, old_value, new_value)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ts, nz(ev.User), ev.Action, ev.Table, nz(ev.RowID), nz(ev.Col), nz(ev.OldValue), nz(ev.NewValue),
	)
	return err
}

// nz maps "" to a NULL so audit columns stay tidy.
func nz(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// ── Delete / restore (tombstone-based, importer-proof) ──────────────────────

// idn-keyed extension tables purged when a whole person is deleted. audit_log is
// deliberately excluded — it is the recovery breadcrumb and must survive.
var extensionTablesByIDN = []string{
	"defendant_notes", "defendant_tags", "court_dates", "violations",
	"reminders", "overrides", "pinned_defendants", "defendant_documents",
	"scheduled_check_ins", "drug_screens", "fee_waivers", "letter_log",
	"custody_periods",
}

// raw tables physically purged only on the IMPORTER_RETIRED path.
var rawTablesByIDN = []string{
	"raw_blue_book", "raw_check_ins", "raw_payments", "raw_gps_48_hours",
}

// DeletePerson tombstones an entire IDN so it disappears from every view and
// stays gone across imports, purges that person's app-owned extension rows, and
// (only when importerRetired) physically deletes their raw_* rows too. One audit
// row records the action. All steps run in a single transaction.
func DeletePerson(d *sql.DB, idn, by, reason string, importerRetired bool) error {
	idn = strings.TrimSpace(idn)
	if idn == "" {
		return errEmptyIDN
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Tombstone (idempotent). Harmless no-op once the importer is retired.
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO deleted_idns (idn, case_number, deleted_by, reason, deleted_at)
		 VALUES (?, NULL, ?, ?, ?)`,
		idn, nz(by), nz(reason), compute.NowET().Format("2006-01-02 15:04:05 MST"),
	); err != nil {
		return err
	}
	// Purge app-owned extension rows for this person.
	for _, t := range extensionTablesByIDN {
		if _, err := tx.Exec("DELETE FROM "+t+" WHERE idn = ?", idn); err != nil {
			return err
		}
	}
	// Cutover only: physical row delete of the source rows.
	if importerRetired {
		for _, t := range rawTablesByIDN {
			if _, err := tx.Exec("DELETE FROM "+t+" WHERE idn = ?", idn); err != nil {
				return err
			}
		}
	}
	action := "delete_person"
	if importerRetired {
		action = "delete_person_physical"
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: action, Table: "deleted_idns", RowID: idn, NewValue: reason,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteCase tombstones a single case token of a (possibly multi-case) person.
// The person and their other cases remain. Person-level extension rows are left
// intact (they aren't case-scoped). On the IMPORTER_RETIRED path the matching
// raw_blue_book / raw_payments rows are physically removed.
func DeleteCase(d *sql.DB, idn, caseTok, by, reason string, importerRetired bool) error {
	idn = strings.TrimSpace(idn)
	caseTok = strings.TrimSpace(caseTok)
	if idn == "" {
		return errEmptyIDN
	}
	if caseTok == "" {
		return errEmptyCase
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO deleted_idns (idn, case_number, deleted_by, reason, deleted_at)
		 VALUES (?, ?, ?, ?, ?)`,
		idn, caseTok, nz(by), nz(reason), compute.NowET().Format("2006-01-02 15:04:05 MST"),
	); err != nil {
		return err
	}
	if importerRetired {
		// Physically drop the blue_book rows whose warrant case matches the token,
		// and payments tagged with that case. Check-ins/GPS are person-scoped and
		// shared across cases, so they're left to the whole-person delete.
		if err := deleteRawByCase(tx, idn, caseTok); err != nil {
			return err
		}
	}
	action := "delete_case"
	if importerRetired {
		action = "delete_case_physical"
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: action, Table: "deleted_idns", RowID: idn, Col: caseTok, NewValue: reason,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// deleteRawByCase physically removes the raw rows matching a single case token
// (IMPORTER_RETIRED path only). Matching is tokenized on /[,\s]+/ in Go since a
// warrant_case_num may group several tokens (e.g. "@A @B").
func deleteRawByCase(tx *sql.Tx, idn, caseTok string) error {
	rows, err := tx.Query("SELECT rowid, warrant_case_num FROM raw_blue_book WHERE idn = ?", idn)
	if err != nil {
		return err
	}
	var rowids []int64
	for rows.Next() {
		var rid int64
		var wc sql.NullString
		if err := rows.Scan(&rid, &wc); err != nil {
			rows.Close()
			return err
		}
		for _, tok := range compute.CaseTokens(wc.String) {
			if tok == strings.ToLower(caseTok) {
				rowids = append(rowids, rid)
				break
			}
		}
	}
	rows.Close()
	for _, rid := range rowids {
		if _, err := tx.Exec("DELETE FROM raw_blue_book WHERE rowid = ?", rid); err != nil {
			return err
		}
	}
	// Payments carry a case_number column directly.
	prows, err := tx.Query("SELECT rowid, case_number FROM raw_payments WHERE idn = ?", idn)
	if err != nil {
		return err
	}
	var prowids []int64
	for prows.Next() {
		var rid int64
		var cn sql.NullString
		if err := prows.Scan(&rid, &cn); err != nil {
			prows.Close()
			return err
		}
		for _, tok := range compute.CaseTokens(cn.String) {
			if tok == strings.ToLower(caseTok) {
				prowids = append(prowids, rid)
				break
			}
		}
	}
	prows.Close()
	for _, rid := range prowids {
		if _, err := tx.Exec("DELETE FROM raw_payments WHERE rowid = ?", rid); err != nil {
			return err
		}
	}
	return nil
}

// RestorePerson un-tombstones a whole person (the "undo last delete" nicety).
// It cannot recover physically-deleted raw rows (IMPORTER_RETIRED), only lift a
// tombstone while the importer still owns the source rows.
func RestorePerson(d *sql.DB, idn, by string) error {
	idn = strings.TrimSpace(idn)
	if idn == "" {
		return errEmptyIDN
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec("DELETE FROM deleted_idns WHERE idn = ? AND case_number IS NULL", idn)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: "restore_person", Table: "deleted_idns", RowID: idn,
		NewValue: "tombstones removed: " + strconv.FormatInt(n, 10),
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// RestoreCase un-tombstones a single case token.
func RestoreCase(d *sql.DB, idn, caseTok, by string) error {
	idn = strings.TrimSpace(idn)
	caseTok = strings.TrimSpace(caseTok)
	if idn == "" || caseTok == "" {
		return errEmptyCase
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM deleted_idns WHERE idn = ? AND case_number = ?", idn, caseTok); err != nil {
		return err
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: "restore_case", Table: "deleted_idns", RowID: idn, Col: caseTok,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// ── Field overrides (write side) ─────────────────────────────────────────────

// SetOverride upserts an (idn, field) override and audits the old/new value.
func SetOverride(d *sql.DB, idn, field, value, by string) error {
	idn = strings.TrimSpace(idn)
	field = strings.TrimSpace(field)
	if idn == "" {
		return errEmptyIDN
	}
	if !overridableFields[field] {
		return errBadField
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var old sql.NullString
	_ = tx.QueryRow("SELECT value FROM overrides WHERE idn = ? AND field = ?", idn, field).Scan(&old)

	now := compute.NowET().Format("2006-01-02 15:04:05 MST")
	if _, err := tx.Exec(
		`INSERT INTO overrides (idn, field, value, author, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(idn, field) DO UPDATE SET value = excluded.value,
		   author = excluded.author, updated_at = excluded.updated_at`,
		idn, field, value, nz(by), now, now,
	); err != nil {
		return err
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: "override_set", Table: "overrides", RowID: idn, Col: field,
		OldValue: old.String, NewValue: value,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// SetGPSDetails records the record-level "Edit GPS details" form in one
// transaction: each supplied GPS/victim field is upserted as an override when
// non-empty, or cleared (reverting to the imported value) when blanked. Only
// fields that actually change are written + audited, so a no-op save is silent.
// Officer-accessible — these are operational monitoring details the import often
// leaves blank, not sensitive corrections.
func SetGPSDetails(d *sql.DB, idn string, vals map[string]string, by string) error {
	return setOverrideFields(d, idn, gpsDetailFields, vals, by, "gps_detail_set", "gps_detail_clear")
}

// SetCaseInfo records the record-level "Edit case info" form: the imported case
// detail (charges, bond, level, supervision/order, officer, name, DMA) plus the
// single-value date fields (referral, closed, DOB). Stored as field overrides —
// importer-proof, audited, reversible — and merged back into every view by
// BuildClients. Officer-accessible (audited), matching the GPS-details editor.
func SetCaseInfo(d *sql.DB, idn string, vals map[string]string, by string) error {
	return setOverrideFields(d, idn, caseInfoFields, vals, by, "case_info_set", "case_info_clear")
}

// setOverrideFields upserts a map of field→value into the overrides table within
// one transaction, restricted to the given allow-list. It writes only CHANGED
// fields (a blank clears via DELETE, an absent key is left untouched, an
// unchanged value is skipped so the audit log stays quiet), recording each write
// under setAction / clearAction. Shared by SetGPSDetails and SetCaseInfo.
func setOverrideFields(d *sql.DB, idn string, fields []string, vals map[string]string, by, setAction, clearAction string) error {
	idn = strings.TrimSpace(idn)
	if idn == "" {
		return errEmptyIDN
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := compute.NowET().Format("2006-01-02 15:04:05 MST")
	for _, field := range fields {
		val, ok := vals[field]
		if !ok {
			continue // field not on the form this submit — leave it untouched
		}
		val = strings.TrimSpace(val)
		var old sql.NullString
		_ = tx.QueryRow("SELECT value FROM overrides WHERE idn = ? AND field = ?", idn, field).Scan(&old)
		if val == old.String && (val != "" || old.Valid) {
			continue // unchanged — no write, no audit noise
		}
		if val == "" {
			if !old.Valid {
				continue // nothing to clear
			}
			if _, err := tx.Exec("DELETE FROM overrides WHERE idn = ? AND field = ?", idn, field); err != nil {
				return err
			}
			if err := WriteAudit(tx, AuditEvent{
				User: by, Action: clearAction, Table: "overrides", RowID: idn, Col: field, OldValue: old.String,
			}); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO overrides (idn, field, value, author, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(idn, field) DO UPDATE SET value = excluded.value,
			   author = excluded.author, updated_at = excluded.updated_at`,
			idn, field, val, nz(by), now, now,
		); err != nil {
			return err
		}
		if err := WriteAudit(tx, AuditEvent{
			User: by, Action: setAction, Table: "overrides", RowID: idn, Col: field,
			OldValue: old.String, NewValue: val,
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClearOverride removes an override, reverting the field to the imported value.
func ClearOverride(d *sql.DB, idn, field, by string) error {
	idn = strings.TrimSpace(idn)
	field = strings.TrimSpace(field)
	if idn == "" || field == "" {
		return errBadField
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old sql.NullString
	_ = tx.QueryRow("SELECT value FROM overrides WHERE idn = ? AND field = ?", idn, field).Scan(&old)
	if _, err := tx.Exec("DELETE FROM overrides WHERE idn = ? AND field = ?", idn, field); err != nil {
		return err
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: "override_clear", Table: "overrides", RowID: idn, Col: field, OldValue: old.String,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// ── small sentinel errors ────────────────────────────────────────────────────

type adminErr string

func (e adminErr) Error() string { return string(e) }

const (
	errEmptyIDN  = adminErr("idn is required")
	errEmptyCase = adminErr("case number is required")
	errBadField  = adminErr("field is not overridable")
)
