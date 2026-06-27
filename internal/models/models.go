// Package models holds the plain data shapes shared across the Go app's
// db / handlers layers. The business-math types live in internal/compute.
package models

// RosterRow is one client on a cross-client roster (Behind on GPS / Missed
// Check-Ins), mirroring the HTML tool's BehindRoster / MissedCheckInsRoster.
type RosterRow struct {
	IDN     string  `json:"idn"`
	Name    string  `json:"name"`
	Officer string  `json:"officer"`
	Level   int     `json:"level"`
	Detail  string  `json:"detail"` // human summary (e.g. "behind $264 / 33 days")
	Amount  float64 `json:"amount"` // surplus dollars (negative = behind), or 0

	// Fee breakdown (populated by behindRoster; zero on the missed-check-in roster)
	// for the console's owed/paid/behind columns (Build-Spec §5.8).
	Owed   float64 `json:"owed"`   // GPS dollars owed to date
	Paid   float64 `json:"paid"`   // GPS dollars paid to date
	Waived bool    `json:"waived"` // fee-waiver note present on the GPS record
}

// Stats is the dashboard summary.
type Stats struct {
	Total       int `json:"total"`
	Open        int `json:"open"`
	Closed      int `json:"closed"`
	GPSActive   int `json:"gpsActive"`
	BehindGPS   int `json:"behindGps"`
	MissedMonth int `json:"missedThisMonth"`
}

// SearchHit is one row of the live lookup.
type SearchHit struct {
	IDN     string `json:"idn"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Level   int    `json:"level"`
	Officer string `json:"officer"`
	CaseNum string `json:"caseNum"`
}

// DefendantRow is one defendant (open-preferred rep per IDN) on the case-
// management grid / the /api/defendants bundle, with their computed compliance.
type DefendantRow struct {
	IDN         string   `json:"idn"`
	Name        string   `json:"name"`
	Level       int      `json:"level"`
	Status      string   `json:"status"`
	Officer     string   `json:"officer"`
	CaseNo      string   `json:"caseNo"`
	GpsActive   bool     `json:"gpsActive"`
	GpsVendor   string   `json:"gpsVendor"`
	GpsSurplus  *float64 `json:"gpsSurplus"`  // nil when uncomputable (MISSING)
	BehindGPS   bool     `json:"behindGps"`   // surplus < 0 (open)
	PTRBalance  float64  `json:"ptrBalance"`  // paid - owed (negative = owes)
	MissedCount int      `json:"missedCount"` // missed check-in windows to date
	MissedMonth bool     `json:"missedThisMonth"`
}

// Bar is one labelled count for the analytics CSS bar charts. Pct is the bar
// width relative to the largest bar in its group (0..100), filled server-side.
type Bar struct {
	Label string `json:"label"`
	Count int    `json:"count"`
	Pct   int    `json:"-"`
}

// Analytics is the server-computed summary for the console Reports page.
type Analytics struct {
	Stats        Stats   `json:"stats"`
	ByLevel      []Bar   `json:"byLevel"`
	ByVendor     []Bar   `json:"byVendor"`
	TopOfficers  []Bar   `json:"topOfficers"`
	TotalGpsOwed float64 `json:"totalGpsOwed"`
	TotalGpsPaid float64 `json:"totalGpsPaid"`
	TotalPtrOwed int     `json:"totalPtrOwed"`
	TotalPtrPaid float64 `json:"totalPtrPaid"`
}

// CalEvent is one dated event on the per-client calendar (ported getEventsForClient).
type CalEvent struct {
	Day   int    `json:"day"`  // day-of-month (1..31) within the rendered month
	Kind  string `json:"kind"` // referral|gps-install|gps-switch|closed|checkin-*|payment|ptr-fee|missed|due
	Label string `json:"label"`
}

// CalDay is one cell in the rendered month grid.
type CalDay struct {
	Day    int // 0 == padding cell before the 1st
	Events []CalEvent
}

// RosterDay is one cell of the roster-mode (team) calendar: aggregated counts
// across all clients for that day. Day 0 == padding cell before the 1st.
type RosterDay struct {
	Day      int `json:"day"`
	CheckIns int `json:"checkIns"`
	Payments int `json:"payments"`
	Court    int `json:"court"`
	Missed   int `json:"missed"`
	Due      int `json:"due"`
}

// RosterTotals is one aggregate bucket of roster-calendar counts — used for a
// week-row total and a weekday-column total (STATUS nice-to-have:
// "roster-calendar weekly/column totals").
type RosterTotals struct {
	CheckIns int `json:"checkIns"`
	Payments int `json:"payments"`
	Court    int `json:"court"`
	Missed   int `json:"missed"`
	Due      int `json:"due"`
}

// Any reports whether the bucket has anything to show (keeps templates clean).
func (t RosterTotals) Any() bool {
	return t.CheckIns+t.Payments+t.Court+t.Missed+t.Due > 0
}

// RosterWeek is one rendered week row: exactly 7 (possibly padded) day cells
// plus that week's totals, shown as a trailing "Week" column.
type RosterWeek struct {
	Days []RosterDay  `json:"days"`
	Tot  RosterTotals `json:"tot"`
}

// RosterCalendar is the rendered roster-mode month: padded day cells + the
// month totals (the "per day / per month" aggregation from Brief 2.9).
// Weeks/ColTotals carry the same days regrouped into week rows with week
// totals, plus per-weekday (Sun..Sat) column totals for the footer row.
type RosterCalendar struct {
	Title       string         `json:"title"`
	Days        []RosterDay    `json:"days"`
	Weeks       []RosterWeek   `json:"weeks"`
	ColTotals   []RosterTotals `json:"colTotals"` // len 7, Sun..Sat
	Month       RosterTotals   `json:"month"`     // grand totals (same as Tot*)
	TotCheckIns int            `json:"totCheckIns"`
	TotPayments int            `json:"totPayments"`
	TotCourt    int            `json:"totCourt"`
	TotMissed   int            `json:"totMissed"`
	TotDue      int            `json:"totDue"`
}

// ── Admin & data-entry shapes (Phase 7) ──────────────────────────────────────

// Note is one free-text note attached to a defendant.
type Note struct {
	ID        int64  `json:"id"`
	IDN       string `json:"idn"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

// Tag is one label on a defendant.
type Tag struct {
	ID        int64  `json:"id"`
	IDN       string `json:"idn"`
	Label     string `json:"label"`
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
}

// CourtDate is one upcoming court date for a defendant. Outcome/NextDate are
// filled after the hearing (FTA-tracking; empty until logged).
type CourtDate struct {
	ID        int64  `json:"id"`
	IDN       string `json:"idn"`
	CourtDate string `json:"courtDate"`
	Court     string `json:"court"`
	Notes     string `json:"notes"`
	Outcome   string `json:"outcome"`
	NextDate  string `json:"nextDate"`
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
}

// Reminder is one per-defendant TODO.
type Reminder struct {
	ID         int64  `json:"id"`
	IDN        string `json:"idn"`
	Body       string `json:"body"`
	DueDate    string `json:"dueDate"`
	AssignedTo string `json:"assignedTo"`
	CreatedBy  string `json:"createdBy"`
	Completed  bool   `json:"completed"`
	CreatedAt  string `json:"createdAt"`
}

// ScheduledCheckIn is one booked future check-in appointment (migration 001
// §12's "calendar of upcoming check-ins"). Display-only with respect to the
// compliance math — the real check-in is logged separately.
type ScheduledCheckIn struct {
	ID        int64  `json:"id"`
	IDN       string `json:"idn"`
	For       string `json:"scheduledFor"`
	Type      string `json:"type"`
	Officer   string `json:"officer"`
	CreatedBy string `json:"createdBy"`
	CreatedAt string `json:"createdAt"`
}

// CustodyPeriod is one in-custody (GPS-off) span for a GPS client. The days from
// Start through the day before End are NOT billed for GPS; End is the "back on
// GPS"/reinstall date and IS billed. End empty = still in custody (excluded
// through today). App-owned + audited; folded into the GPS billing math.
type CustodyPeriod struct {
	ID        int64  `json:"id"`
	IDN       string `json:"idn"`
	Start     string `json:"start"`
	End       string `json:"end"`
	Note      string `json:"note"`
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
}

// Violation is one recorded violation.
type Violation struct {
	ID            int64  `json:"id"`
	IDN           string `json:"idn"`
	ViolationDate string `json:"violationDate"`
	Category      string `json:"category"`
	Severity      string `json:"severity"`
	Description   string `json:"description"`
	ActionTaken   string `json:"actionTaken"`
	Officer       string `json:"officer"`
	CreatedAt     string `json:"createdAt"`
}

// DrugScreen is one recorded drug-screen result (officer CRUD, audited). A
// failed screen is typically also recorded as a violation (category
// failed-drug-screen); this table is the per-test log behind that.
type DrugScreen struct {
	ID         int64  `json:"id"`
	IDN        string `json:"idn"`
	ScreenDate string `json:"screenDate"`
	TestType   string `json:"testType"`   // urine / oral swab / hair / breath / other
	Result     string `json:"result"`     // negative / positive / diluted / refused / pending
	Substances string `json:"substances"` // when positive, what it was positive for
	Notes      string `json:"notes"`
	Officer    string `json:"officer"`
	CreatedAt  string `json:"createdAt"`
}

// SavedView is one per-user saved roster filter combo ("saved search"). Query
// is a sanitized URL query string of the roster's filter params.
type SavedView struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Query     string `json:"query"`
	CreatedAt string `json:"createdAt"`
}

// Tombstone is one row of deleted_idns — a suppressed person or case.
type Tombstone struct {
	IDN        string `json:"idn"`
	CaseNumber string `json:"caseNumber"` // "" = whole person
	DeletedBy  string `json:"deletedBy"`
	DeletedAt  string `json:"deletedAt"`
	Reason     string `json:"reason"`
	Name       string `json:"name"` // resolved from raw_blue_book when still present
}

// Override is one active field override on a defendant, for the profile panel.
type Override struct {
	Field     string `json:"field"` // raw column key
	Label     string `json:"label"` // friendly label
	Value     string `json:"value"` // overriding value
	Author    string `json:"author"`
	UpdatedAt string `json:"updatedAt"`
}

// ReportGroup is one grouped section of a report — e.g. one supervising
// officer's clients on the per-officer Behind report. When Report.Groups is
// non-empty the template renders one table per group (with its subtotal line)
// instead of the flat Rows table; Rows still carries the flat list so the
// record count and CSV export stay consistent.
type ReportGroup struct {
	Label    string     `json:"label"`
	Rows     [][]string `json:"rows"`
	Subtotal string     `json:"subtotal"`
}

// Report is a generic printable/exportable tabular report (rendered by
// report.html). The same shape backs Behind-on-GPS, Missed-Check-Ins, etc.
type Report struct {
	Title    string        `json:"title"`
	Subtitle string        `json:"subtitle"`
	AsOf     string        `json:"asOf"`
	Columns  []string      `json:"columns"`
	Rows     [][]string    `json:"rows"`
	Groups   []ReportGroup `json:"groups,omitempty"` // per-officer split etc.
	CSVPath  string        `json:"csvPath"`          // link to the matching CSV export, if any
	Note     string        `json:"note"`             // optional footnote (e.g. show-cause-letter status)
	AltLabel string        `json:"altLabel"`         // label for the alternate view link, if any
	AltURL   string        `json:"altUrl"`           // URL for the alternate view (flat ⇄ grouped)
}

// AuditRow is one audit_log entry for the supervisor audit viewer.
type AuditRow struct {
	Ts       string `json:"ts"`
	User     string `json:"user"`
	Action   string `json:"action"`
	Table    string `json:"table"`
	RowID    string `json:"rowId"`
	Col      string `json:"col"`
	OldValue string `json:"oldValue"`
	NewValue string `json:"newValue"`
}

// AddedPayment is one app-entered payment (Phase 10 data entry), shown on the
// profile so an officer can confirm or delete a mistaken entry. Imported payments
// are not listed here — they surface via the computed fee summaries.
type AddedPayment struct {
	ID            int64  `json:"id"`
	IDN           string `json:"idn"`
	CaseNumber    string `json:"caseNumber"`
	PaymentDate   string `json:"paymentDate"`
	PaymentAmount string `json:"paymentAmount"`
	PaymentType   string `json:"paymentType"`
	Officer       string `json:"officer"`
	Author        string `json:"author"`
	CreatedAt     string `json:"createdAt"`
}

// AddedCheckIn is one app-entered check-in (Phase 10 data entry).
type AddedCheckIn struct {
	ID        int64  `json:"id"`
	IDN       string `json:"idn"`
	Date      string `json:"date"`
	Type      string `json:"type"`
	Note      string `json:"note"` // optional per-check-in note (e.g. GPS fitment details)
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
}

// DefendantExtras bundles a defendant's app-owned data for the profile page.
type DefendantExtras struct {
	Notes             []Note
	Tags              []Tag
	CourtDates        []CourtDate
	Reminders         []Reminder
	Violations        []Violation
	DrugScreens       []DrugScreen
	Overrides         []Override
	AddedPayments     []AddedPayment
	AddedCheckIns     []AddedCheckIn
	ScheduledCheckIns []ScheduledCheckIn
	Letters           []LetterLogEntry
	CustodyPeriods    []CustodyPeriod
}

// LetterLogEntry is one generated letter from letter_log (migration 007) —
// read-only history surfaced on the record's Activity timeline; generation
// happens on the EM-fees report.
type LetterLogEntry struct {
	ID          int64  `json:"id"`
	IDN         string `json:"idn"`
	Case        string `json:"case"`
	Type        string `json:"type"`
	Detail      string `json:"detail"`
	GeneratedBy string `json:"generatedBy"`
	CreatedAt   string `json:"createdAt"`
}

// AppUser is one row of the app_users table — the in-app roles & permissions
// roster (migration 010). Role is one of officer | supervisor | admin.
type AppUser struct {
	Email     string `json:"email"`
	Role      string `json:"role"`
	AddedBy   string `json:"addedBy"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// ChatMessage is one line in the group chat (app-owned; not from raw_*).
// Author is the sender's email; Created is an RFC3339 timestamp in ET.
type ChatMessage struct {
	ID      int64  `json:"id"`
	Author  string `json:"author"`
	Body    string `json:"body"`
	Created string `json:"created"`
}

// ── QR self-check-in (migration 011) ────────────────────────────────────────

// ClientContact is the structured phone + home address for a client, used by
// the QR self-check-in flow: the phone is the SMS-OTP destination (proof of
// possession of the registered handset) and the home lat/lng is what an
// approving officer's "checked in from their own house" comparison runs
// against. App-owned + audited; the intake wizard is the write path going
// forward. PhoneE164 is stored canonicalized ("+18655551234").
type ClientContact struct {
	IDN             string  `json:"idn"`
	PhoneE164       string  `json:"phoneE164"`
	PhoneVerifiedAt string  `json:"phoneVerifiedAt"`
	AddressLine1    string  `json:"addressLine1"`
	AddressLine2    string  `json:"addressLine2"`
	City            string  `json:"city"`
	State           string  `json:"state"`
	Zip             string  `json:"zip"`
	HomeLat         float64 `json:"homeLat"`
	HomeLng         float64 `json:"homeLng"`
	HasHomeGeo      bool    `json:"hasHomeGeo"` // false until an address is geocoded
	UpdatedBy       string  `json:"updatedBy"`
	UpdatedAt       string  `json:"updatedAt"`
}

// WeeklyCode is one rotating check-in code (migration 011). The QR posted in
// the lobby encodes the currently-active code; it does not *block* an at-home
// check-in by itself (a client keeps a working URL for the week) — it's a
// hygiene + provenance signal: a submission stamped with an expired/old code is
// auto-flagged, and the code can be reprinted each week.
type WeeklyCode struct {
	ID        int64  `json:"id"`
	Code      string `json:"code"`
	Label     string `json:"label"` // "Week of Jun 22, 2026"
	ValidFrom string `json:"validFrom"`
	ValidTo   string `json:"validTo"`
	Active    bool   `json:"active"`
	CreatedBy string `json:"createdBy"`
	CreatedAt string `json:"createdAt"`
}

// CheckinFlag is one machine-derived suspicion/quality flag on a Checkin
// (e.g. "off_site", "stale_code", "gps_denied", "new_device",
// "impossible_travel", "matches_home"). Stored as a JSON array on the row and
// surfaced to the approving officer.
type CheckinFlag string

// Checkin is one self-service check-in submission — the tamper-evident
// evidence record that (once approved) stands in for the paper Pre-Trial
// Release Reporting Form. Append-only: corrections are new rows, never edits.
// Telemetry is split into server-observed (trustworthy — the app recorded it
// directly off the connection) and client-supplied (corroborating — the phone
// handed it over and a determined person could spoof it). The hash chain
// (PrevHash → RecordHash) makes post-hoc alteration provable.
type Checkin struct {
	ID         int64  `json:"id"`
	IDN        string `json:"idn"`
	Status     string `json:"status"` // pending | approved | rejected
	ReportType string `json:"reportType"`

	// Client-confirmed snapshot of the reporting form.
	ClientName       string `json:"clientName"`
	Phone            string `json:"phone"`
	AddressLine1     string `json:"addressLine1"`
	AddressLine2     string `json:"addressLine2"`
	City             string `json:"city"`
	State            string `json:"state"`
	Zip              string `json:"zip"`
	EmploymentStatus string `json:"employmentStatus"`
	Employer         string `json:"employer"`
	UnemployedLength string `json:"unemployedLength"`
	CitationSince    bool   `json:"citationSince"`
	CitationDate     string `json:"citationDate"`
	ArrestedSince    bool   `json:"arrestedSince"`
	ArrestedDate     string `json:"arrestedDate"`
	NextCourtDate    string `json:"nextCourtDate"`
	SignatureKind    string `json:"signatureKind"` // typed | drawn
	SignatureData    string `json:"signatureData"` // typed name, or PNG data URL

	// Consent (what they agreed to, when).
	ConsentVersion string `json:"consentVersion"`
	ConsentText    string `json:"consentText"`
	ConsentAt      string `json:"consentAt"`

	// Server-observed telemetry (trustworthy).
	ServerTS      string `json:"serverTs"`
	SrcIP         string `json:"srcIp"`
	IPCity        string `json:"ipCity"`
	IPRegion      string `json:"ipRegion"`
	IPISP         string `json:"ipIsp"`
	WeekCodeID    int64  `json:"weekCodeId"`
	WeekCodeValid bool   `json:"weekCodeValid"`

	// Client-supplied telemetry (corroborating).
	ClientTS    string  `json:"clientTs"`
	GPSLat      float64 `json:"gpsLat"`
	GPSLng      float64 `json:"gpsLng"`
	GPSAccuracy float64 `json:"gpsAccuracyM"`
	GPSPerm     string  `json:"gpsPerm"` // granted | denied | unavailable
	Timezone    string  `json:"timezone"`
	Locale      string  `json:"locale"`
	UserAgent   string  `json:"userAgent"`
	DeviceID    string  `json:"deviceId"`

	// Identity factors.
	OTPPhoneMask   string `json:"otpPhoneMask"`
	OTPVerifiedAt  string `json:"otpVerifiedAt"`
	SelfiePath     string `json:"selfiePath"`
	SelfieLiveness string `json:"selfieLiveness"` // passed | failed | skipped

	// Computed presence assessment.
	DistOfficeM   float64 `json:"distOfficeM"`
	DistHomeM     float64 `json:"distHomeM"`
	PresenceBadge string  `json:"presenceBadge"` // green | yellow | red
	Flags         string  `json:"flags"`         // JSON array of CheckinFlag

	// Tamper-evidence (sha256 chain over the canonical record).
	PrevHash   string `json:"prevHash"`
	RecordHash string `json:"recordHash"`

	// Review.
	ApprovedBy   string `json:"approvedBy"`
	ApprovedAt   string `json:"approvedAt"`
	RejectReason string `json:"rejectReason"`

	CreatedAt string `json:"createdAt"`
}

// ClientFlag is a manual alert an officer raises on a client — a prominent
// "pay attention to this person" marker (e.g. safety risk, absconding risk, do
// not release). App-owned + audited; shown as a banner on the record and a chip
// on the roster until another officer clears it. Severity is "red" (urgent) or
// "amber" (caution).
type ClientFlag struct {
	ID        int64  `json:"id"`
	IDN       string `json:"idn"`
	Severity  string `json:"severity"` // red | amber
	Reason    string `json:"reason"`
	CreatedBy string `json:"createdBy"`
	CreatedAt string `json:"createdAt"`
}

// Bulletin is one post on the office-wide notice board shown on the check-in
// page — a persistent announcement (unlike the 7-day group chat) every officer
// sees: policy reminders, "court closed Friday", "watch for X". App-owned +
// audited; high-priority/pinned posts sort to the top.
type Bulletin struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Priority  string `json:"priority"` // normal | high
	Pinned    bool   `json:"pinned"`
	CreatedBy string `json:"createdBy"`
	CreatedAt string `json:"createdAt"`
}
