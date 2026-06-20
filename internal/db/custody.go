// custody.go — in-custody (GPS-off) periods for GPS clients. Days inside a period
// are excluded from GPS billing; the period's end ("back on GPS") date is the
// reinstall day and IS billed. App-owned (importer-proof), every mutation audited,
// purged on whole-person delete (custody_periods is in extensionTablesByIDN), and
// folded into the GPS math via BuildClients (Client.Custody) + ComputeGPS and into
// the EM-fee letters via EMFees.
package db

import (
	"database/sql"
	"strings"

	"pretrial-knoxc/internal/compute"
	"pretrial-knoxc/internal/emfees"
	"pretrial-knoxc/internal/models"
)

const custodySelect = `SELECT custody_id, idn, start_date, IFNULL(end_date,''),
       IFNULL(note,''), IFNULL(author,''), IFNULL(created_at,'')
  FROM custody_periods`

func scanCustody(rows *sql.Rows) ([]models.CustodyPeriod, error) {
	defer rows.Close()
	var out []models.CustodyPeriod
	for rows.Next() {
		var c models.CustodyPeriod
		if err := rows.Scan(&c.ID, &c.IDN, &c.Start, &c.End, &c.Note, &c.Author, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListCustodyPeriods returns one client's custody periods, most recent first
// (start_date is yyyy-mm-dd from the date input, so TEXT order is chronological).
func ListCustodyPeriods(d *sql.DB, idn string) ([]models.CustodyPeriod, error) {
	if !tableExists(d, "custody_periods") {
		return nil, nil
	}
	rows, err := d.Query(custodySelect+` WHERE idn = ? ORDER BY start_date DESC, custody_id DESC`, idn)
	if err != nil {
		return nil, err
	}
	return scanCustody(rows)
}

// AddCustodyPeriod records an in-custody span (any allowed officer; audited).
// end may be empty ("still in custody"). start is required.
func AddCustodyPeriod(d *sql.DB, idn, start, end, note, by string) error {
	idn, start = strings.TrimSpace(idn), strings.TrimSpace(start)
	if idn == "" || start == "" {
		return errEmptyField
	}
	return txAddWithAudit(d,
		AuditEvent{User: by, Action: "custody_add", Table: "custody_periods", RowID: idn, NewValue: start + "→" + strings.TrimSpace(end)},
		`INSERT INTO custody_periods (idn, start_date, end_date, note, author)
		 VALUES (?, ?, ?, ?, ?)`,
		idn, start, nz(end), nz(note), nz(by))
}

// DeleteCustodyPeriod removes a custody span (audited).
func DeleteCustodyPeriod(d *sql.DB, id int64, by string) error {
	return txDeleteByID(d, "custody_periods", "custody_id", id, by, "custody_delete")
}

// loadCustodyForCompute reads every custody period and returns idn ->
// []compute.CustodyPeriod (dates parsed to noon-UTC), for BuildClients to attach
// to each Client so ComputeGPS can exclude the days. Tolerant of an absent table.
func loadCustodyForCompute(d *sql.DB) (map[string][]compute.CustodyPeriod, error) {
	if !tableExists(d, "custody_periods") {
		return nil, nil
	}
	rows, err := d.Query(custodySelect)
	if err != nil {
		return nil, err
	}
	recs, err := scanCustody(rows)
	if err != nil {
		return nil, err
	}
	out := map[string][]compute.CustodyPeriod{}
	for _, r := range recs {
		idn := norm(r.IDN)
		if idn == "" {
			continue
		}
		s, sok := compute.ParseDay(r.Start)
		if !sok {
			continue // a period with no parseable start can't bound anything
		}
		e, eok := compute.ParseDay(r.End)
		out[idn] = append(out[idn], compute.CustodyPeriod{Start: s, End: e, StartOK: true, EndOK: eok})
	}
	return out, nil
}

// loadCustodyForEMFees reads every custody period as raw date strings keyed by
// IDN, for the EM-fee engine (which parses dates with its own parseDate). Tolerant
// of an absent table.
func loadCustodyForEMFees(d *sql.DB) (map[string][]emfees.CustodyRange, error) {
	if !tableExists(d, "custody_periods") {
		return nil, nil
	}
	rows, err := d.Query(custodySelect)
	if err != nil {
		return nil, err
	}
	recs, err := scanCustody(rows)
	if err != nil {
		return nil, err
	}
	out := map[string][]emfees.CustodyRange{}
	for _, r := range recs {
		idn := norm(r.IDN)
		if idn == "" {
			continue
		}
		out[idn] = append(out[idn], emfees.CustodyRange{Start: r.Start, End: r.End})
	}
	return out, nil
}
