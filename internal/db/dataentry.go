// dataentry.go is the write side of Phase 10 — adding defendants, payments, and
// check-ins from the website. Like every other app write (Brief 5.4), it touches
// ONLY app-owned extension tables (added_defendants / added_payments /
// added_check_ins), never raw_*. Those rows are merged into BuildClients,
// LookupDatasets, and EMFees (see the merges there), so an app-added record is a
// first-class citizen of every view and survives the importer's Sunday reload.
// Every insert/delete writes one audit_log breadcrumb.
package db

import (
	"database/sql"
	"strings"

	"pretrial-knoxc/internal/models"
)

var (
	errExistingIDN = adminErr("a client with that IDN already exists — open their profile to add a case, payment, or check-in")
	errEmptyName   = adminErr("defendant name is required")
)

// NewDefendant is the field set for adding a client. IDN + Name are required;
// everything else is optional. Status defaults to "Open".
type NewDefendant struct {
	IDN, Name, CaseNumber, Level, Status, Officer, ReferralDate string
	GPS, GPSType, ChargeType, BondAmount, SupervisionType       string
	OrderFrom, DMA, Birthdate                                   string
}

// IDNExistsInRoster reports whether an IDN is already present in the imported
// roster or among app-added defendants (so we don't create a duplicate person).
func IDNExistsInRoster(d *sql.DB, idn string) bool {
	idn = strings.TrimSpace(idn)
	if idn == "" {
		return false
	}
	for _, tbl := range []string{"raw_blue_book", "added_defendants"} {
		if !tableExists(d, tbl) {
			continue
		}
		var got string
		err := d.QueryRow("SELECT idn FROM "+tbl+" WHERE TRIM(idn) = ? LIMIT 1", idn).Scan(&got)
		if err == nil {
			return true
		}
	}
	return false
}

// AddDefendant inserts a new client. Rejects a blank IDN/Name and any IDN already
// in the roster. Returns the audited insert in one transaction.
func AddDefendant(d *sql.DB, nd NewDefendant, by string) error {
	nd.IDN = strings.TrimSpace(nd.IDN)
	nd.Name = strings.TrimSpace(nd.Name)
	if nd.IDN == "" {
		return errEmptyIDN
	}
	if nd.Name == "" {
		return errEmptyName
	}
	if IDNExistsInRoster(d, nd.IDN) {
		return errExistingIDN
	}
	status := strings.TrimSpace(nd.Status)
	if status == "" {
		status = "Open"
	}
	gps := "False"
	if isTruthy(nd.GPS) {
		gps = "True"
	}
	return txAddWithAudit(d,
		AuditEvent{User: by, Action: "defendant_add", Table: "added_defendants", RowID: nd.IDN, NewValue: clip(nd.Name)},
		`INSERT INTO added_defendants
		   (idn, defendant, warrant_case_num, pretrial_level, case_status, supervising_officer,
		    referral_date, gps, gps_type, charge_type, bond_amount, supervision_type,
		    order_from, dma, birthdate, author)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		nd.IDN, nd.Name, strings.TrimSpace(nd.CaseNumber), strings.TrimSpace(nd.Level), status,
		strings.TrimSpace(nd.Officer), strings.TrimSpace(nd.ReferralDate), gps, strings.TrimSpace(nd.GPSType),
		strings.TrimSpace(nd.ChargeType), strings.TrimSpace(nd.BondAmount), strings.TrimSpace(nd.SupervisionType),
		strings.TrimSpace(nd.OrderFrom), strings.TrimSpace(nd.DMA), strings.TrimSpace(nd.Birthdate), nz(by))
}

// AddPayment records a payment against an existing IDN.
func AddPayment(d *sql.DB, idn, caseNumber, date, amount, ptype, officer, by string) error {
	idn = strings.TrimSpace(idn)
	if idn == "" {
		return errEmptyIDN
	}
	if strings.TrimSpace(amount) == "" || strings.TrimSpace(date) == "" {
		return errEmptyField
	}
	return txAddWithAudit(d,
		AuditEvent{User: by, Action: "payment_add", Table: "added_payments", RowID: idn,
			NewValue: clip(strings.TrimSpace(ptype) + " $" + strings.TrimSpace(amount) + " " + strings.TrimSpace(date))},
		`INSERT INTO added_payments
		   (idn, case_number, payment_date, payment_amount, payment_type, officer_that_collected_payment, author)
		 VALUES (?,?,?,?,?,?,?)`,
		idn, strings.TrimSpace(caseNumber), strings.TrimSpace(date), strings.TrimSpace(amount),
		strings.TrimSpace(ptype), strings.TrimSpace(officer), nz(by))
}

// AddCheckIn records a check-in against an existing IDN.
func AddCheckIn(d *sql.DB, idn, date, ctype, by string) error {
	idn = strings.TrimSpace(idn)
	if idn == "" {
		return errEmptyIDN
	}
	if strings.TrimSpace(date) == "" {
		return errEmptyField
	}
	return txAddWithAudit(d,
		AuditEvent{User: by, Action: "checkin_add", Table: "added_check_ins", RowID: idn,
			NewValue: clip(strings.TrimSpace(ctype) + " " + strings.TrimSpace(date))},
		`INSERT INTO added_check_ins (idn, date, type_of_check_in, author) VALUES (?,?,?,?)`,
		idn, strings.TrimSpace(date), strings.TrimSpace(ctype), nz(by))
}

func DeleteAddedPayment(d *sql.DB, id int64, by string) error {
	return txDeleteByID(d, "added_payments", "add_id", id, by, "payment_delete")
}

func DeleteAddedCheckIn(d *sql.DB, id int64, by string) error {
	return txDeleteByID(d, "added_check_ins", "add_id", id, by, "checkin_delete")
}

// ListAddedPayments returns app-entered payments for a client (newest first), so
// the profile can show and let an officer delete a mistaken entry. Imported
// payments are not listed here — they appear via the computed fee summaries.
func ListAddedPayments(d *sql.DB, idn string) ([]models.AddedPayment, error) {
	if !tableExists(d, "added_payments") {
		return nil, nil
	}
	rows, err := d.Query(
		`SELECT add_id, idn, IFNULL(case_number,''), IFNULL(payment_date,''), IFNULL(payment_amount,''),
		        IFNULL(payment_type,''), IFNULL(officer_that_collected_payment,''), IFNULL(author,''), IFNULL(created_at,'')
		 FROM added_payments WHERE TRIM(idn) = ? ORDER BY add_id DESC`, strings.TrimSpace(idn))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AddedPayment
	for rows.Next() {
		var p models.AddedPayment
		if err := rows.Scan(&p.ID, &p.IDN, &p.CaseNumber, &p.PaymentDate, &p.PaymentAmount, &p.PaymentType, &p.Officer, &p.Author, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListAddedCheckIns returns app-entered check-ins for a client (newest first).
func ListAddedCheckIns(d *sql.DB, idn string) ([]models.AddedCheckIn, error) {
	if !tableExists(d, "added_check_ins") {
		return nil, nil
	}
	rows, err := d.Query(
		`SELECT add_id, idn, IFNULL(date,''), IFNULL(type_of_check_in,''), IFNULL(author,''), IFNULL(created_at,'')
		 FROM added_check_ins WHERE TRIM(idn) = ? ORDER BY add_id DESC`, strings.TrimSpace(idn))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AddedCheckIn
	for rows.Next() {
		var c models.AddedCheckIn
		if err := rows.Scan(&c.ID, &c.IDN, &c.Date, &c.Type, &c.Author, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "on", "y":
		return true
	}
	return false
}
