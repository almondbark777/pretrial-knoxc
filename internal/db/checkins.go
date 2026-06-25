// checkins.go is the data layer for QR self-check-in (migration 011): the
// structured client_contact record (phone + home address) the flow depends on,
// the rotating weekly codes, and the append-only, hash-chained `checkins`
// evidence table that — once an officer approves it — stands in for the paper
// Pre-Trial Release Reporting Form.
//
// Two principles the rest of the feature leans on:
//   - Append-only. A check-in is never edited; a correction is a new row.
//     Approve/reject only stamp the review columns, never the captured data.
//   - Tamper-evidence. Every insert chains sha256(prev_hash + canonical(row))
//     so any later byte-level alteration breaks the chain and is provable. This
//     is what lets a records custodian testify the record is unaltered.
package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strconv"
	"strings"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/models"
)

// ── checkin_config (key/value flags) ────────────────────────────────────────

// checkin_config defaults. background_location_enabled stays "0": the
// continuous-location capability is built but fenced off until a court
// authorizes it (Carpenter). office_lat/lng/geofence back the presence badge.
var checkinConfigDefaults = map[string]string{
	"background_location_enabled": "0",
	"sms_otp_enabled":             "0",       // capability built (internal/otp); off until Twilio + 10DLC are live
	"office_lat":                  "35.9646", // 300 Main St, Knoxville TN (approx; set precisely in admin)
	"office_lng":                  "-83.9202",
	"geofence_radius_m":           "150",
	"consent_version":             "2026-06-25",
}

// GetCheckinConfig returns a config value, falling back to the built-in default.
func GetCheckinConfig(d *sql.DB, key string) string {
	var v string
	if err := d.QueryRow(`SELECT value FROM checkin_config WHERE key = ?`, key).Scan(&v); err == nil && v != "" {
		return v
	}
	return checkinConfigDefaults[key]
}

// GetCheckinConfigFloat is GetCheckinConfig parsed as a float (0 on garbage).
func GetCheckinConfigFloat(d *sql.DB, key string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(GetCheckinConfig(d, key)), 64)
	return f
}

// SetCheckinConfig upserts one config flag (audited). Supervisor-gated at the
// handler — flipping background_location_enabled is a court-authorization act.
func SetCheckinConfig(d *sql.DB, key, value, by string) error {
	if strings.TrimSpace(key) == "" {
		return errEmptyField
	}
	return txAddWithAudit(d,
		AuditEvent{User: by, Action: "checkin_config_set", Table: "checkin_config", RowID: key, NewValue: clip(value)},
		`INSERT INTO checkin_config (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
}

// ── client_contact ──────────────────────────────────────────────────────────

// GetClientContact returns the structured phone/address for a client, or
// (nil, nil) when none has been recorded yet (the common pre-rollout case).
func GetClientContact(d *sql.DB, idn string) (*models.ClientContact, error) {
	row := d.QueryRow(`
		SELECT idn,
		       IFNULL(phone_e164,''), IFNULL(phone_verified_at,''),
		       IFNULL(address_line1,''), IFNULL(address_line2,''),
		       IFNULL(city,''), IFNULL(state,''), IFNULL(zip,''),
		       home_lat, home_lng,
		       IFNULL(updated_by,''), IFNULL(updated_at,'')
		  FROM client_contact WHERE idn = ?`, strings.TrimSpace(idn))
	var c models.ClientContact
	var lat, lng sql.NullFloat64
	if err := row.Scan(&c.IDN, &c.PhoneE164, &c.PhoneVerifiedAt,
		&c.AddressLine1, &c.AddressLine2, &c.City, &c.State, &c.Zip,
		&lat, &lng, &c.UpdatedBy, &c.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if lat.Valid && lng.Valid {
		c.HomeLat, c.HomeLng, c.HasHomeGeo = lat.Float64, lng.Float64, true
	}
	return &c, nil
}

// UpsertClientContact writes (or updates) a client's structured contact info.
// Home lat/lng are optional — pass hasGeo=false to leave them untouched/NULL so
// a later geocode pass can fill them without clobbering an officer's edit.
func UpsertClientContact(d *sql.DB, c models.ClientContact, hasGeo bool, by string) error {
	idn := strings.TrimSpace(c.IDN)
	if idn == "" {
		return errEmptyField
	}
	var lat, lng any
	if hasGeo {
		lat, lng = c.HomeLat, c.HomeLng
	}
	return txAddWithAudit(d,
		AuditEvent{User: by, Action: "client_contact_set", Table: "client_contact", RowID: idn, NewValue: clip(c.PhoneE164)},
		`INSERT INTO client_contact
		   (idn, phone_e164, address_line1, address_line2, city, state, zip, home_lat, home_lng, updated_by, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(idn) DO UPDATE SET
		   phone_e164    = excluded.phone_e164,
		   address_line1 = excluded.address_line1,
		   address_line2 = excluded.address_line2,
		   city          = excluded.city,
		   state         = excluded.state,
		   zip           = excluded.zip,
		   home_lat      = COALESCE(excluded.home_lat, client_contact.home_lat),
		   home_lng      = COALESCE(excluded.home_lng, client_contact.home_lng),
		   updated_by    = excluded.updated_by,
		   updated_at    = CURRENT_TIMESTAMP`,
		idn, nz(c.PhoneE164), nz(c.AddressLine1), nz(c.AddressLine2),
		nz(c.City), nz(c.State), nz(c.Zip), lat, lng, nz(by))
}

// ── checkin_weekly_codes ────────────────────────────────────────────────────

// ActiveWeeklyCode returns the current lobby code (active, newest), or nil if
// none has been minted yet.
func ActiveWeeklyCode(d *sql.DB) (*models.WeeklyCode, error) {
	row := d.QueryRow(`
		SELECT code_id, code, IFNULL(label,''), valid_from, valid_to, active, IFNULL(created_by,''), IFNULL(created_at,'')
		  FROM checkin_weekly_codes
		 WHERE active = 1
		 ORDER BY code_id DESC LIMIT 1`)
	return scanWeeklyCode(row)
}

// WeeklyCodeByCode looks up a submitted code so the handler can stamp the
// check-in with its id and decide week_code_valid (expired/unknown → flagged).
func WeeklyCodeByCode(d *sql.DB, code string) (*models.WeeklyCode, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, nil
	}
	row := d.QueryRow(`
		SELECT code_id, code, IFNULL(label,''), valid_from, valid_to, active, IFNULL(created_by,''), IFNULL(created_at,'')
		  FROM checkin_weekly_codes WHERE code = ?`, code)
	return scanWeeklyCode(row)
}

// CreateWeeklyCode mints a new code and deactivates prior ones (one active code
// at a time). Returns the new code_id.
func CreateWeeklyCode(d *sql.DB, code, label, validFrom, validTo, by string) (int64, error) {
	code = strings.TrimSpace(code)
	if code == "" || strings.TrimSpace(validFrom) == "" || strings.TrimSpace(validTo) == "" {
		return 0, errEmptyField
	}
	tx, err := d.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE checkin_weekly_codes SET active = 0 WHERE active = 1`); err != nil {
		return 0, err
	}
	res, err := tx.Exec(
		`INSERT INTO checkin_weekly_codes (code, label, valid_from, valid_to, active, created_by)
		 VALUES (?, ?, ?, ?, 1, ?)`,
		code, nz(label), validFrom, validTo, nz(by))
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if err := WriteAudit(tx, AuditEvent{User: by, Action: "weekcode_create", Table: "checkin_weekly_codes",
		RowID: strconv.FormatInt(id, 10), NewValue: label}); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func scanWeeklyCode(row *sql.Row) (*models.WeeklyCode, error) {
	var w models.WeeklyCode
	var active int
	if err := row.Scan(&w.ID, &w.Code, &w.Label, &w.ValidFrom, &w.ValidTo, &active, &w.CreatedBy, &w.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	w.Active = active != 0
	return &w, nil
}

// ── checkins (append-only, hash-chained) ────────────────────────────────────

// checkinCols is the SELECT list shared by every read, IFNULL-wrapped so a row
// with sparse telemetry scans into plain Go types.
const checkinCols = `
	checkin_id, idn, IFNULL(status,'pending'), IFNULL(report_type,''),
	IFNULL(client_name,''), IFNULL(phone,''),
	IFNULL(address_line1,''), IFNULL(address_line2,''), IFNULL(city,''), IFNULL(state,''), IFNULL(zip,''),
	IFNULL(employment_status,''), IFNULL(employer,''), IFNULL(unemployed_length,''),
	IFNULL(citation_since,0), IFNULL(citation_date,''), IFNULL(arrested_since,0), IFNULL(arrested_date,''),
	IFNULL(next_court_date,''), IFNULL(signature_kind,''), IFNULL(signature_data,''),
	IFNULL(consent_version,''), IFNULL(consent_text,''), IFNULL(consent_at,''),
	IFNULL(server_ts,''), IFNULL(src_ip,''), IFNULL(ip_city,''), IFNULL(ip_region,''), IFNULL(ip_isp,''),
	IFNULL(week_code_id,0), IFNULL(week_code_valid,1),
	IFNULL(client_ts,''), IFNULL(gps_lat,0), IFNULL(gps_lng,0), IFNULL(gps_accuracy_m,0), IFNULL(gps_perm,''),
	IFNULL(timezone,''), IFNULL(locale,''), IFNULL(user_agent,''), IFNULL(device_id,''),
	IFNULL(otp_phone_mask,''), IFNULL(otp_verified_at,''), IFNULL(selfie_path,''), IFNULL(selfie_liveness,''),
	IFNULL(dist_office_m,0), IFNULL(dist_home_m,0), IFNULL(presence_badge,''), IFNULL(flags,''),
	IFNULL(prev_hash,''), IFNULL(record_hash,''),
	IFNULL(approved_by,''), IFNULL(approved_at,''), IFNULL(reject_reason,''), IFNULL(created_at,'')`

type rowScanner interface{ Scan(dest ...any) error }

func scanCheckin(s rowScanner) (models.Checkin, error) {
	var c models.Checkin
	var citation, arrested, weekValid int
	if err := s.Scan(
		&c.ID, &c.IDN, &c.Status, &c.ReportType,
		&c.ClientName, &c.Phone,
		&c.AddressLine1, &c.AddressLine2, &c.City, &c.State, &c.Zip,
		&c.EmploymentStatus, &c.Employer, &c.UnemployedLength,
		&citation, &c.CitationDate, &arrested, &c.ArrestedDate,
		&c.NextCourtDate, &c.SignatureKind, &c.SignatureData,
		&c.ConsentVersion, &c.ConsentText, &c.ConsentAt,
		&c.ServerTS, &c.SrcIP, &c.IPCity, &c.IPRegion, &c.IPISP,
		&c.WeekCodeID, &weekValid,
		&c.ClientTS, &c.GPSLat, &c.GPSLng, &c.GPSAccuracy, &c.GPSPerm,
		&c.Timezone, &c.Locale, &c.UserAgent, &c.DeviceID,
		&c.OTPPhoneMask, &c.OTPVerifiedAt, &c.SelfiePath, &c.SelfieLiveness,
		&c.DistOfficeM, &c.DistHomeM, &c.PresenceBadge, &c.Flags,
		&c.PrevHash, &c.RecordHash,
		&c.ApprovedBy, &c.ApprovedAt, &c.RejectReason, &c.CreatedAt,
	); err != nil {
		return c, err
	}
	c.CitationSince, c.ArrestedSince, c.WeekCodeValid = citation != 0, arrested != 0, weekValid != 0
	return c, nil
}

// InsertCheckin appends one submission and returns its new id and record hash.
// The hash chains off the most recent row's record_hash (a table-wide chain),
// computed inside the same transaction so the single-writer SQLite serialization
// guarantees no interleave. Status is forced to 'pending' — approval is a
// separate, audited act.
func InsertCheckin(d *sql.DB, c models.Checkin) (int64, string, error) {
	if strings.TrimSpace(c.IDN) == "" || strings.TrimSpace(c.ServerTS) == "" {
		return 0, "", errEmptyField
	}
	tx, err := d.Begin()
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()

	var prev string
	_ = tx.QueryRow(`SELECT IFNULL(record_hash,'') FROM checkins ORDER BY checkin_id DESC LIMIT 1`).Scan(&prev)
	c.PrevHash = prev
	c.RecordHash = computeCheckinHash(prev, c)

	res, err := tx.Exec(`
		INSERT INTO checkins (
			idn, status, report_type,
			client_name, phone, address_line1, address_line2, city, state, zip,
			employment_status, employer, unemployed_length,
			citation_since, citation_date, arrested_since, arrested_date, next_court_date,
			signature_kind, signature_data,
			consent_version, consent_text, consent_at,
			server_ts, src_ip, ip_city, ip_region, ip_isp, week_code_id, week_code_valid,
			client_ts, gps_lat, gps_lng, gps_accuracy_m, gps_perm, timezone, locale, user_agent, device_id,
			otp_phone_mask, otp_verified_at, selfie_path, selfie_liveness,
			dist_office_m, dist_home_m, presence_badge, flags,
			prev_hash, record_hash
		) VALUES (
			?, 'pending', ?,
			?, ?, ?, ?, ?, ?, ?,
			?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?,
			?, ?, ?,
			?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?
		)`,
		strings.TrimSpace(c.IDN), nz(c.ReportType),
		nz(c.ClientName), nz(c.Phone), nz(c.AddressLine1), nz(c.AddressLine2), nz(c.City), nz(c.State), nz(c.Zip),
		nz(c.EmploymentStatus), nz(c.Employer), nz(c.UnemployedLength),
		b2i(c.CitationSince), nz(c.CitationDate), b2i(c.ArrestedSince), nz(c.ArrestedDate), nz(c.NextCourtDate),
		nz(c.SignatureKind), nz(c.SignatureData),
		nz(c.ConsentVersion), nz(c.ConsentText), nz(c.ConsentAt),
		c.ServerTS, nz(c.SrcIP), nz(c.IPCity), nz(c.IPRegion), nz(c.IPISP), nfid(c.WeekCodeID), b2i(c.WeekCodeValid),
		nz(c.ClientTS), gpsArg(c.GPSPerm, c.GPSLat), gpsArg(c.GPSPerm, c.GPSLng), gpsArg(c.GPSPerm, c.GPSAccuracy), nz(c.GPSPerm),
		nz(c.Timezone), nz(c.Locale), nz(c.UserAgent), nz(c.DeviceID),
		nz(c.OTPPhoneMask), nz(c.OTPVerifiedAt), nz(c.SelfiePath), nz(c.SelfieLiveness),
		c.DistOfficeM, c.DistHomeM, nz(c.PresenceBadge), nz(c.Flags),
		nz(c.PrevHash), c.RecordHash,
	)
	if err != nil {
		return 0, "", err
	}
	id, _ := res.LastInsertId()
	if err := WriteAudit(tx, AuditEvent{User: "self-checkin", Action: "checkin_submit", Table: "checkins",
		RowID: strconv.FormatInt(id, 10), NewValue: c.RecordHash[:12]}); err != nil {
		return 0, "", err
	}
	return id, c.RecordHash, tx.Commit()
}

// ListPendingCheckins returns submissions awaiting review, oldest first (FIFO
// queue). Selfie data and consent text are heavy; callers that only need the
// queue can ignore them.
func ListPendingCheckins(d *sql.DB) ([]models.Checkin, error) {
	return queryCheckins(d, `SELECT `+checkinCols+` FROM checkins WHERE status = 'pending' ORDER BY created_at ASC`)
}

// ListCheckinsForIDN returns a client's check-in history, newest first.
func ListCheckinsForIDN(d *sql.DB, idn string) ([]models.Checkin, error) {
	return queryCheckins(d, `SELECT `+checkinCols+` FROM checkins WHERE idn = ? ORDER BY created_at DESC`, strings.TrimSpace(idn))
}

// CountPendingCheckins is the cheap COUNT behind the nav badge — how many
// submissions are waiting for an officer to approve.
func CountPendingCheckins(d *sql.DB) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM checkins WHERE status = 'pending'`).Scan(&n)
	return n, err
}

// GetCheckin loads one submission by id (for the review panel + court packet).
func GetCheckin(d *sql.DB, id int64) (*models.Checkin, error) {
	c, err := scanCheckin(d.QueryRow(`SELECT `+checkinCols+` FROM checkins WHERE checkin_id = ?`, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// ApproveCheckin stamps the review columns; the captured data is never touched.
func ApproveCheckin(d *sql.DB, id int64, by string) error {
	return reviewCheckin(d, id, by, "approved", "", "checkin_approve")
}

// RejectCheckin marks a submission rejected with a reason (e.g. "off-site —
// pinged client's home address").
func RejectCheckin(d *sql.DB, id int64, by, reason string) error {
	return reviewCheckin(d, id, by, "rejected", reason, "checkin_reject")
}

func reviewCheckin(d *sql.DB, id int64, by, status, reason, action string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := compute.NowET().Format("2006-01-02 15:04:05 MST")
	if _, err := tx.Exec(
		`UPDATE checkins SET status = ?, approved_by = ?, approved_at = ?, reject_reason = ?
		   WHERE checkin_id = ? AND status = 'pending'`,
		status, nz(by), ts, nz(reason), id); err != nil {
		return err
	}
	if err := WriteAudit(tx, AuditEvent{User: by, Action: action, Table: "checkins",
		RowID: strconv.FormatInt(id, 10), NewValue: status, OldValue: clip(reason)}); err != nil {
		return err
	}
	return tx.Commit()
}

func queryCheckins(d *sql.DB, query string, args ...any) ([]models.Checkin, error) {
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Checkin
	for rows.Next() {
		c, err := scanCheckin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// computeCheckinHash returns sha256(prev || canonical(record)) as hex. The
// canonical form is an explicit, ordered field dump — deterministic regardless
// of struct/JSON ordering — covering exactly the captured evidence (never the
// review columns, which are stamped after the hash is sealed).
func computeCheckinHash(prev string, c models.Checkin) string {
	var b strings.Builder
	w := func(parts ...string) {
		for _, p := range parts {
			b.WriteString(p)
			b.WriteByte('\n')
		}
	}
	w(prev, c.IDN, c.ReportType, c.ClientName, c.Phone,
		c.AddressLine1, c.AddressLine2, c.City, c.State, c.Zip,
		c.EmploymentStatus, c.Employer, c.UnemployedLength,
		bs(c.CitationSince), c.CitationDate, bs(c.ArrestedSince), c.ArrestedDate, c.NextCourtDate,
		c.SignatureKind, c.SignatureData,
		c.ConsentVersion, c.ConsentText, c.ConsentAt,
		c.ServerTS, c.SrcIP, c.IPCity, c.IPRegion, c.IPISP,
		strconv.FormatInt(c.WeekCodeID, 10), bs(c.WeekCodeValid),
		c.ClientTS, fs(c.GPSLat), fs(c.GPSLng), fs(c.GPSAccuracy), c.GPSPerm,
		c.Timezone, c.Locale, c.UserAgent, c.DeviceID,
		c.OTPPhoneMask, c.OTPVerifiedAt, c.SelfiePath, c.SelfieLiveness,
		fs(c.DistOfficeM), fs(c.DistHomeM), c.PresenceBadge, c.Flags)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// VerifyCheckinChain recomputes the table-wide hash chain and returns the id of
// the first row whose stored hash doesn't match (0 = chain intact). The court
// custodian's "this record is unaltered" check.
func VerifyCheckinChain(d *sql.DB) (int64, error) {
	rows, err := d.Query(`SELECT ` + checkinCols + ` FROM checkins ORDER BY checkin_id ASC`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	prev := ""
	for rows.Next() {
		c, err := scanCheckin(rows)
		if err != nil {
			return 0, err
		}
		if computeCheckinHash(prev, c) != c.RecordHash {
			return c.ID, nil
		}
		prev = c.RecordHash
	}
	return 0, rows.Err()
}

// ── small helpers ───────────────────────────────────────────────────────────

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func bs(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func fs(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// nfid maps a 0 week-code id to NULL (no code), else the id.
func nfid(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// gpsArg stores a GPS value only when location was actually granted, so a
// denied/unavailable submission records NULL rather than a misleading 0,0.
func gpsArg(perm string, v float64) any {
	if strings.EqualFold(strings.TrimSpace(perm), "granted") {
		return v
	}
	return nil
}
