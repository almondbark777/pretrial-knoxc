// Package db is the native-SQLite data layer for the Go rewrite. It reads the
// raw_* tables directly (the same source the live lookup tool consumes via
// queries_ext.lookup_datasets) and builds compute.Client objects — mirroring
// the canonical buildClients() join. No T-SQL shim: all queries are native
// SQLite via modernc.org/sqlite (pure Go, no CGO).
package db

import (
	"database/sql"
	"strconv"
	"strings"
	"time"

	"pretrial-knoxc/internal/compute"

	_ "modernc.org/sqlite"
)

// Open opens the SQLite database (read-mostly) with WAL-friendly pragmas.
func Open(path string) (*sql.DB, error) {
	// modernc driver name is "sqlite". busy_timeout avoids transient lock errors
	// while the importer writes.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1) // SQLite: single writer; reads are fast enough serialized here
	if err := d.Ping(); err != nil {
		return nil, err
	}
	return d, nil
}

// queryMaps runs SELECT * and returns each row as a map keyed by column name.
// Tolerates tables that lack optional columns (e.g. switched_to) — a missing
// column simply isn't in the map. Mirrors lookup_datasets()'s tolerance.
func queryMaps(d *sql.DB, table string) ([]map[string]string, error) {
	rows, err := d.Query("SELECT * FROM " + table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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

// queryMapsIfExists is queryMaps that returns (nil, nil) when the table is absent,
// so the added_* data-entry tables are optional on a DB that predates migration 004.
func queryMapsIfExists(d *sql.DB, table string) ([]map[string]string, error) {
	if !tableExists(d, table) {
		return nil, nil
	}
	return queryMaps(d, table)
}

func asStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "1"
		}
		return "0"
	default:
		return ""
	}
}

func toNum(s string) float64 {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' {
			b.WriteRune(r)
		}
	}
	f, err := strconv.ParseFloat(b.String(), 64)
	if err != nil {
		return 0
	}
	return f
}

func norm(s string) string { return strings.TrimSpace(s) }

// BuildClients joins the four raw_* tables into compute.Client objects, grouped
// by IDN. Mirrors the canonical buildClients(): one client object PER blue_book
// row (so multi-case defendants keep every case with its own level/referral/
// status), all sharing the IDN's check-ins/payments and the install-nonempty
// GPS record. The returned map is idn -> []*Client in blue_book row order.
// Callers pick a representative (see handlers.openRep) or a specific case.
func BuildClients(d *sql.DB, track time.Time) (map[string][]*compute.Client, error) {
	bb, err := queryMaps(d, "raw_blue_book")
	if err != nil {
		return nil, err
	}
	ci, err := queryMaps(d, "raw_check_ins")
	if err != nil {
		return nil, err
	}
	pm, err := queryMaps(d, "raw_payments")
	if err != nil {
		return nil, err
	}
	gp, err := queryMaps(d, "raw_gps_48_hours")
	if err != nil {
		return nil, err
	}

	// App-added rows (Phase 10 data entry) are merged in alongside the imported
	// rows so an app-entered defendant/payment/check-in is a first-class member of
	// every view and computation, and survives the Sunday raw_* full reload. They
	// share the raw_* column names, so this is a plain append; tombstones below
	// still suppress them.
	if extra, err := queryMapsIfExists(d, "added_defendants"); err != nil {
		return nil, err
	} else {
		bb = append(bb, extra...)
	}
	if extra, err := queryMapsIfExists(d, "added_check_ins"); err != nil {
		return nil, err
	} else {
		ci = append(ci, extra...)
	}
	if extra, err := queryMapsIfExists(d, "added_payments"); err != nil {
		return nil, err
	} else {
		pm = append(pm, extra...)
	}

	// Admin suppression + corrections, loaded once per build (Phase 7). Tombstoned
	// idns/cases are filtered out below so a deleted person/case vanishes from every
	// view and stays gone across the importer's Sunday full reload; overrides splice
	// supervisor typo-fixes into the raw row after the read.
	tomb, err := loadTombstones(d)
	if err != nil {
		return nil, err
	}
	overrides, err := loadOverrides(d)
	if err != nil {
		return nil, err
	}
	waivers, err := loadFeeWaivers(d)
	if err != nil {
		return nil, err
	}
	custody, err := loadCustodyForCompute(d) // in-custody days excluded from GPS billing
	if err != nil {
		return nil, err
	}

	// GPS map: install-nonempty row wins per idn.
	gpMap := map[string]map[string]string{}
	for _, r := range gp {
		k := norm(r["idn"])
		if k == "" {
			continue
		}
		cur, ok := gpMap[k]
		has := norm(r["gps_install_date"]) != ""
		curHas := ok && norm(cur["gps_install_date"]) != ""
		if !ok || (has && !curHas) {
			gpMap[k] = r
		}
	}

	// check-ins / payments grouped by idn.
	ciMap := map[string][]compute.CheckIn{}
	for _, r := range ci {
		k := norm(r["idn"])
		if k == "" {
			continue
		}
		dt, ok := compute.ParseDay(r["date"])
		ciMap[k] = append(ciMap[k], compute.CheckIn{D: dt, DOK: ok, Type: r["type_of_check_in"]})
	}
	pmMap := map[string][]compute.Payment{}
	for _, r := range pm {
		k := norm(r["idn"])
		if k == "" {
			continue
		}
		dt, ok := compute.ParseDay(r["payment_date"])
		pmMap[k] = append(pmMap[k], compute.Payment{
			D: dt, DOK: ok, Amt: toNum(r["payment_amount"]), Type: r["payment_type"],
			Case: r["case_number"],
		})
	}

	clients := map[string][]*compute.Client{}
	for _, r := range bb {
		idn := norm(r["idn"])
		if idn == "" {
			continue
		}
		// Tombstone filter: whole person, or this specific case.
		if tomb.whole[idn] {
			continue
		}
		caseNo := norm(firstNonEmpty(r["warrant_case_num"], r["case_number"]))
		if tomb.caseSuppressed(idn, caseNo) {
			continue
		}
		// Apply any field overrides for this idn by splicing them into the raw row
		// before it's read, so every downstream value (level/ref/officer/…) and the
		// compute layer already see the corrected value. ovIdn is recorded on the
		// Client so the UI can flag the overridden fields "override (app)".
		ovIdn := overrides[idn]
		for f, v := range ovIdn {
			r[f] = v
		}

		gpRec := gpMap[idn]
		gpsRaw := strings.ToLower(norm(r["gps"]))
		gpsActive := gpsRaw == "true" || gpsRaw == "yes" || gpsRaw == "1" || gpRec != nil

		gpsType := norm(r["gps_type"])
		if gpsType == "" && gpRec != nil {
			gpsType = norm(gpRec["gps_type"])
		}

		c := &compute.Client{
			IDN:       idn,
			Name:      norm(firstNonEmpty(r["defendant"], r["name"])),
			Level:     norm(r["pretrial_level"]),
			Status:    norm(r["case_status"]),
			Officer:   compute.FmtOfficer(norm(r["supervising_officer"])),
			CaseNo:    caseNo,
			GpsActive: gpsActive,
			GpsType:   gpsType,
			DayAdj:    toNum(r["day_adjustment"]),
			CheckIns:  ciMap[idn],
			Payments:  pmMap[idn],
			Overrides: ovIdn,
			Custody:   custody[idn],

			ChargeType:      norm(r["charge_type"]),
			BondAmount:      norm(r["bond_amount"]),
			SupervisionType: norm(r["supervision_type"]),
			OrderFrom:       norm(r["order_from"]),
			DMA:             norm(r["dma"]),
			Birthdate:       norm(r["birthdate"]),
		}
		if gpRec != nil {
			c.GpInstall = norm(gpRec["gps_install_date"])
			c.GpSwitchedTo = norm(gpRec["switched_to"]) // "" if column absent
			c.GpSwitchedDate = norm(gpRec["switched_gps_date"])
			c.GpNotes = norm(gpRec["notes"])
		}
		// An app fee waiver lands in the GPS notes so the one true waiver check
		// (compute.IsFeesWaived) sees it everywhere — record chip, roster Waived
		// flag — with no second flag to keep in sync.
		if m := waivers[idn]; m != "" {
			c.GpNotes = appendGpNote(c.GpNotes, m)
		}
		if dt, ok := compute.ParseDay(norm(r["referral_date"])); ok {
			c.RefD, c.RefOK = dt, true
		}
		if dt, ok := compute.ParseDateTime(norm(r["referral_date"])); ok {
			c.RefDT, c.RefDTOK = dt, true
		}
		if dt, ok := compute.ParseDay(norm(r["closed_date"])); ok {
			c.ClosedD, c.ClosedOK = dt, true
		}
		clients[idn] = append(clients[idn], c) // keep every case row for this idn
	}
	return clients, nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
