package db

import (
	"database/sql"
	"sort"
	"time"

	"pretrial-knoxc/internal/compute"
)

// ledger.go assembles a client's FULL check-in and payment history — the raw
// imported rows (raw_check_ins / raw_payments) merged with the app-entered ones
// (added_check_ins / added_payments) — so the console client record can show the
// complete ledger the bundled tracker shows, not just the app-entered subset.
//
// This is read-only and person-scoped (every case's check-ins/payments together),
// exactly like the tracker's ci/pm feeds. Whole-person tombstones suppress it (a
// deleted client never reaches the record page, but we filter defensively so a
// stray row can't leak).
//
// Per-case tombstones (a single deleted case of a multi-case person) additionally
// drop that case's PAYMENTS from the ledger (payments carry a reliable
// case_number column, matched via tomb.caseSuppressed). Check-ins are NOT
// case-filtered: they are person-scoped in the source data with no reliable case
// column (raw check-ins share a person's whole history across cases), so a
// per-case delete leaves all check-ins visible — only the whole-person tombstone
// (handled above) removes them.

// LedgerCheckIn is one check-in in the full history.
type LedgerCheckIn struct {
	Date    string // display-formatted ("Jan 2, 2006") when parseable, else raw
	Type    string
	Officer string // raw supervising officer (imported) or author (app) — view formats
	Note    string // app-entered fitment/observation note (imported rows have none)
	Case    string
	Source  string // "Imported" | "App"
	sortT   time.Time
	dok     bool
}

// LedgerPayment is one payment in the full history.
type LedgerPayment struct {
	Date    string
	Amount  string // raw amount string — view formats as money
	Type    string
	Officer string
	Case    string
	Source  string // "Imported" | "App"
	sortT   time.Time
	dok     bool
}

// Ledger is a client's complete check-in + payment history, newest first.
type Ledger struct {
	CheckIns []LedgerCheckIn
	Payments []LedgerPayment
}

// fmtDay formats a stored date for display, tolerant of the source data's mixed
// formats (US, ISO, with/without time). Falls back to the raw string.
func fmtDay(s string) (string, time.Time, bool) {
	s = norm(s)
	if s == "" {
		return "—", time.Time{}, false
	}
	if dt, ok := compute.ParseDay(s); ok {
		return dt.Format("Jan 2, 2006"), dt, true
	}
	return s, time.Time{}, false
}

// ClientLedger returns the full check-in + payment history for an IDN, raw +
// app-entered, newest first. Undated rows sort to the bottom (stable).
func ClientLedger(d *sql.DB, idn string) (Ledger, error) {
	idn = norm(idn)
	var lg Ledger
	if idn == "" {
		return lg, nil
	}
	tomb, err := loadTombstones(d)
	if err != nil {
		return lg, err
	}
	if tomb.whole[idn] {
		return lg, nil // deleted person: no ledger
	}

	// ── Check-ins ──
	rawCI, err := queryMapsWhereIDN(d, "raw_check_ins", idn)
	if err != nil {
		return lg, err
	}
	for _, r := range rawCI {
		disp, t, ok := fmtDay(r["date"])
		lg.CheckIns = append(lg.CheckIns, LedgerCheckIn{
			Date: disp, Type: r["type_of_check_in"], Officer: r["supervising_officer"],
			Case: r["case_number"], Source: "Imported", sortT: t, dok: ok,
		})
	}
	appCI, err := queryMapsWhereIDNIfExists(d, "added_check_ins", idn)
	if err != nil {
		return lg, err
	}
	for _, r := range appCI {
		disp, t, ok := fmtDay(r["date"])
		lg.CheckIns = append(lg.CheckIns, LedgerCheckIn{
			Date: disp, Type: r["type_of_check_in"], Officer: r["author"],
			Note: r["note"], Source: "App", sortT: t, dok: ok,
		})
	}

	// ── Payments ──
	rawPM, err := queryMapsWhereIDN(d, "raw_payments", idn)
	if err != nil {
		return lg, err
	}
	for _, r := range rawPM {
		if tomb.caseSuppressed(idn, r["case_number"]) {
			continue // payment belongs to a per-case tombstoned case
		}
		disp, t, ok := fmtDay(r["payment_date"])
		lg.Payments = append(lg.Payments, LedgerPayment{
			Date: disp, Amount: r["payment_amount"], Type: r["payment_type"],
			Officer: r["officer_that_collected_payment"], Case: r["case_number"],
			Source: "Imported", sortT: t, dok: ok,
		})
	}
	appPM, err := queryMapsWhereIDNIfExists(d, "added_payments", idn)
	if err != nil {
		return lg, err
	}
	for _, r := range appPM {
		if tomb.caseSuppressed(idn, r["case_number"]) {
			continue // payment belongs to a per-case tombstoned case
		}
		disp, t, ok := fmtDay(r["payment_date"])
		officer := norm(r["officer_that_collected_payment"])
		if officer == "" {
			officer = r["author"]
		}
		lg.Payments = append(lg.Payments, LedgerPayment{
			Date: disp, Amount: r["payment_amount"], Type: r["payment_type"],
			Officer: officer, Case: r["case_number"], Source: "App", sortT: t, dok: ok,
		})
	}

	sort.SliceStable(lg.CheckIns, func(i, j int) bool {
		a, b := lg.CheckIns[i], lg.CheckIns[j]
		if a.dok != b.dok {
			return a.dok // dated rows before undated
		}
		return a.sortT.After(b.sortT)
	})
	sort.SliceStable(lg.Payments, func(i, j int) bool {
		a, b := lg.Payments[i], lg.Payments[j]
		if a.dok != b.dok {
			return a.dok
		}
		return a.sortT.After(b.sortT)
	})
	return lg, nil
}

// queryMapsWhereIDN runs SELECT * ... WHERE idn = ? (idn column is present on every
// raw_*/added_* table). table is a code-supplied constant; idn is parameterized.
func queryMapsWhereIDN(d *sql.DB, table, idn string) ([]map[string]string, error) {
	rows, err := d.Query("SELECT * FROM "+table+" WHERE idn = ?", idn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMaps(rows)
}

// queryMapsWhereIDNIfExists is queryMapsWhereIDN that tolerates an absent table
// (the added_* tables predate migration 004 on older DBs).
func queryMapsWhereIDNIfExists(d *sql.DB, table, idn string) ([]map[string]string, error) {
	if !tableExists(d, table) {
		return nil, nil
	}
	return queryMapsWhereIDN(d, table, idn)
}

// scanMaps turns an open *sql.Rows into []map[string]string (column -> value),
// shared by queryMaps and the WHERE variants.
func scanMaps(rows *sql.Rows) ([]map[string]string, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]string
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]string, len(cols))
		for i, c := range cols {
			m[c] = asStr(raw[i])
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
