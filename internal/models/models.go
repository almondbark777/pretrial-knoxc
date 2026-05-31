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

// Analytics is the server-computed summary for analytics.html.
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

// MyDay is the logged-in officer's personal worklist: their caseload's
// due-this-week / behind / missed items (reuses RosterRow).
type MyDay struct {
	Officer  string      `json:"officer"`  // display name matched against client.Officer
	Caseload int         `json:"caseload"` // distinct clients supervised by this officer
	DueSoon  []RosterRow `json:"dueSoon"`  // check-in window due within 7 days
	Behind   []RosterRow `json:"behind"`   // behind on GPS
	Missed   []RosterRow `json:"missed"`   // missed a check-in this month
}

// RosterDay is one cell of the roster-mode (team) calendar: aggregated counts
// across all clients for that day. Day 0 == padding cell before the 1st.
type RosterDay struct {
	Day      int `json:"day"`
	CheckIns int `json:"checkIns"`
	Payments int `json:"payments"`
	Missed   int `json:"missed"`
	Due      int `json:"due"`
}

// RosterCalendar is the rendered roster-mode month: padded day cells + the
// month totals (the "per day / per month" aggregation from Brief 2.9).
type RosterCalendar struct {
	Title       string      `json:"title"`
	Days        []RosterDay `json:"days"`
	TotCheckIns int         `json:"totCheckIns"`
	TotPayments int         `json:"totPayments"`
	TotMissed   int         `json:"totMissed"`
	TotDue      int         `json:"totDue"`
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

// CourtDate is one upcoming court date for a defendant.
type CourtDate struct {
	ID        int64  `json:"id"`
	IDN       string `json:"idn"`
	CourtDate string `json:"courtDate"`
	Court     string `json:"court"`
	Notes     string `json:"notes"`
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

// DefendantExtras bundles a defendant's app-owned data for the profile page.
type DefendantExtras struct {
	Notes      []Note
	Tags       []Tag
	CourtDates []CourtDate
	Reminders  []Reminder
	Violations []Violation
	Overrides  []Override
}
