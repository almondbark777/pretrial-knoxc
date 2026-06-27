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

// norm trims an imported value and strips the stringified-list wrapper some
// SharePoint / Power Automate multi-choice columns arrive in — e.g.
// `["Pre-Trial"]` → `Pre-Trial`, `["#4 SCRAM","#7 Supervision"]` →
// `#4 SCRAM, #7 Supervision`. A plain value (the common case) is returned as-is.
func norm(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		s = unwrapList(s)
	}
	return s
}

// unwrapList flattens a `["a","b"]`-style import artifact into `a, b` (quotes
// stripped, empties dropped). Splitting on comma is safe because the parts are
// re-joined with ", ", so a comma inside a value reproduces the same display.
func unwrapList(s string) string {
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return ""
	}
	var out []string
	for _, p := range strings.Split(inner, ",") {
		if p = strings.Trim(strings.TrimSpace(p), `"'`); strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimSpace(p))
		}
	}
	return strings.Join(out, ", ")
}

// canonSupervisionType folds the legacy "Pre-Trial" / "Pre Trial" import spelling
// to the app's "Pretrial" (the supervision-type dropdown value), so existing data
// matches what officers pick. Any other value passes through unchanged.
func canonSupervisionType(s string) string {
	flat := strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(s)))
	if flat == "pretrial" {
		return "Pretrial"
	}
	return s
}

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
	notBehind, err := loadNotBehindAcks(d) // "reviewed — not behind" holds (report #12)
	if err != nil {
		return nil, err
	}
	custody, err := loadCustodyForCompute(d) // in-custody days excluded from GPS billing
	if err != nil {
		return nil, err
	}

	// GPS records grouped per idn — KEEP EVERY ROW. GPS is per CASE, not per idn:
	// a person can have multiple GPS records across cases (one open + several
	// closed). Each blue-book case is matched to its own record below
	// (gpsRecordForCase). Previously this kept a single "install-nonempty wins" row
	// per idn, which shared one install window across all of the idn's cases and
	// discarded removal info recorded on the other rows — over-billing closed-out
	// cases and keeping the GPS tag lit after a client was taken off GPS.
	gpByIDN := map[string][]map[string]string{}
	for _, r := range gp {
		k := norm(r["idn"])
		if k == "" {
			continue
		}
		gpByIDN[k] = append(gpByIDN[k], r)
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

		gpsRecs := gpByIDN[idn]
		gpRec := gpsRecordForCase(gpsRecs, caseNo)
		// gpsField reads a GPS/victim detail with app-override precedence: an app
		// override wins, else THIS CASE's GPS 48-hour record, else the blue-book /
		// added-defendant row. Lets officers fill in or correct vendor / install /
		// switch / victim 48-hour values that the import left blank — importer-proof
		// & audited.
		gpsField := func(col string) string {
			if v := norm(ovIdn[col]); v != "" {
				return v
			}
			if gpRec != nil {
				if v := norm(gpRec[col]); v != "" {
					return v
				}
			}
			return norm(r[col])
		}

		gpsType := gpsField("gps_type")
		gpsRaw := strings.ToLower(strings.TrimSpace(norm(r["gps"])))
		multiRec := len(gpsRecs) > 1
		// Removed from GPS: a relief switch ("no gps" / "off gps" / "removed") on the
		// record, OR an explicit officer "mark off GPS" override (gps_removed) for
		// when the import never recorded a removal row. Clears the GPS-Monitored tag
		// (problem report #11) and, in ComputeGPS, stops billing at the switch date.
		// Date-independent so the build cache stays as-of-independent.
		gpsRemovedFlag := strings.ToLower(strings.TrimSpace(gpsField("gps_removed")))
		removed := compute.IsReliefSwitch(gpsField("switched_to")) ||
			gpsRemovedFlag == "true" || gpsRemovedFlag == "yes" || gpsRemovedFlag == "1"
		// A case can explicitly declare it is NOT a GPS case (gps flag False). Honor
		// that only for multi-record idns (true per-case GPS) so we never strip the
		// tag from a single-record GPS client whose import flag is a stale False.
		explicitOff := multiRec && (gpsRaw == "false" || gpsRaw == "no" || gpsRaw == "0")
		// On GPS if the flag says so, this case has a GPS record, or an app override
		// supplied a vendor / install date — unless it's been removed or flagged off.
		hasSignal := gpsRaw == "true" || gpsRaw == "yes" || gpsRaw == "1" || gpRec != nil ||
			gpsType != "" || gpsField("gps_install_date") != ""
		gpsActive := hasSignal && !removed && !explicitOff

		c := &compute.Client{
			IDN:       idn,
			Name:      norm(firstNonEmpty(r["defendant"], r["name"])),
			Level:     norm(r["pretrial_level"]),
			Status:    norm(r["case_status"]),
			Officer:   compute.FmtOfficer(norm(r["supervising_officer"])),
			CaseNo:    caseNo,
			GpsActive: gpsActive,
			NotBehind: notBehind[idn],
			GpsType:   gpsType,
			DayAdj:    toNum(r["day_adjustment"]),
			CheckIns:  ciMap[idn],
			Payments:  pmMap[idn],
			Overrides: ovIdn,
			Custody:   custody[idn],

			ChargeType:      norm(r["charge_type"]),
			BondAmount:      norm(r["bond_amount"]),
			SupervisionType: canonSupervisionType(norm(r["supervision_type"])),
			OrderFrom:       norm(r["order_from"]),
			DMA:             norm(r["dma"]),
			Birthdate:       norm(r["birthdate"]),
		}
		c.GpInstall = gpsField("gps_install_date")
		c.GpSwitchedTo = gpsField("switched_to") // "" if column absent everywhere
		c.GpSwitchedDate = gpsField("switched_gps_date")
		c.GpDAEmailed = gpsField("da_emailed")
		c.GpCourtOrder = gpsField("court_order")
		c.VictimNotify48 = gpsField("victim_time_48")
		c.VictimAcceptDeny = gpsField("victim_accept_deny_gps")
		c.Victim = gpsField("victim")
		c.VictimIDN = gpsField("victim_idn")
		c.Victim2 = gpsField("victim_2")
		c.Victim2IDN = gpsField("victim_2_idn")
		c.Victim3 = gpsField("victim_3")
		c.Victim3IDN = gpsField("victim_3_idn")
		if gpRec != nil {
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

// gpsRecordForCase picks the GPS 48-hour record(s) that apply to a blue-book
// case. With 0 or 1 record for the idn it returns that record as-is (legacy: a
// single GPS stint shared by the idn's case rows). With multiple records — the
// person has GPS across more than one case — it returns only the record(s) whose
// case tokens overlap this case, MERGED, so an install row and a later
// removal/switch row for the same case combine into one complete record (the
// import writes a removal as a separate row). Returns nil when no record applies.
func gpsRecordForCase(recs []map[string]string, caseNo string) map[string]string {
	switch len(recs) {
	case 0:
		return nil
	case 1:
		return recs[0]
	}
	toks := compute.CaseTokens(caseNo)
	var matched []map[string]string
	for _, r := range recs {
		if caseToksOverlap(toks, compute.CaseTokens(norm(r["case_number"]))) {
			matched = append(matched, r)
		}
	}
	if len(matched) == 0 {
		return nil
	}
	return mergeGpsRecords(matched)
}

// caseToksOverlap reports whether two token slices share any token.
func caseToksOverlap(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// mergeGpsRecords combines GPS rows for the same case into one map, taking the
// first non-empty value per column. The import records a GPS removal/switch as a
// separate row (install on one, switched_to/date/notes on another), so merging
// recovers the removal info the old one-row-per-idn pick dropped.
func mergeGpsRecords(recs []map[string]string) map[string]string {
	if len(recs) == 1 {
		return recs[0]
	}
	out := map[string]string{}
	for _, r := range recs {
		for k, v := range r {
			if strings.TrimSpace(out[k]) == "" && strings.TrimSpace(v) != "" {
				out[k] = v
			}
		}
	}
	return out
}
