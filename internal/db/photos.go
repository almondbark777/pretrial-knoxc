// photos.go — defendant / victim photos for the client record (problem report
// #10). Images are stored base64-encoded in the DB (same approach as
// checkin_media) so the deploy stays a single binary and uploads aren't at the
// mercy of the box's flaky uplink for a separate file store. App-owned table,
// one audit_log breadcrumb per add/delete, purged on whole-person delete.
package db

import (
	"database/sql"
	"encoding/base64"
	"strings"

	"pretrial-knoxc/internal/compute"
)

// DefendantPhoto is one stored image's metadata (no bytes — those are streamed
// separately by GetDefendantPhoto so list views stay light).
type DefendantPhoto struct {
	ID      int64
	IDN     string
	Kind    string // "defendant" | "victim"
	Mime    string
	Caption string
	Author  string
	Created string
}

// PhotoKind normalizes a submitted kind to the two allowed values; anything else
// (including blank) defaults to "defendant".
func PhotoKind(k string) string {
	if strings.EqualFold(strings.TrimSpace(k), "victim") {
		return "victim"
	}
	return "defendant"
}

// SaveDefendantPhoto stores one image (base64) for an idn and audits it.
func SaveDefendantPhoto(d *sql.DB, idn, kind, mime, caption string, raw []byte, by string) error {
	idn = strings.TrimSpace(idn)
	if idn == "" || len(raw) == 0 {
		return errEmptyField
	}
	kind = PhotoKind(kind)
	b64 := base64.StdEncoding.EncodeToString(raw)
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`INSERT INTO defendant_photos (idn, kind, mime, caption, image_b64, author, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		idn, kind, nz(mime), nz(strings.TrimSpace(caption)), b64, nz(by),
		compute.NowET().Format("2006-01-02 15:04:05 MST")); err != nil {
		return err
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: "photo_add", Table: "defendant_photos", RowID: idn, NewValue: kind,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// ListDefendantPhotos returns photo metadata for an idn, newest first.
func ListDefendantPhotos(d *sql.DB, idn string) ([]DefendantPhoto, error) {
	var out []DefendantPhoto
	if !tableExists(d, "defendant_photos") {
		return out, nil
	}
	rows, err := d.Query(
		`SELECT photo_id, idn, kind, COALESCE(mime,''), COALESCE(caption,''),
		        COALESCE(author,''), created_at
		   FROM defendant_photos WHERE TRIM(idn) = ? ORDER BY photo_id DESC`,
		strings.TrimSpace(idn))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p DefendantPhoto
		if err := rows.Scan(&p.ID, &p.IDN, &p.Kind, &p.Mime, &p.Caption, &p.Author, &p.Created); err != nil {
			return out, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetDefendantPhoto returns one image's raw bytes + mime + owning idn for serving.
func GetDefendantPhoto(d *sql.DB, id int64) (raw []byte, mime, idn string, err error) {
	var b64 string
	row := d.QueryRow(
		`SELECT COALESCE(mime,''), image_b64, idn FROM defendant_photos WHERE photo_id = ?`, id)
	if err = row.Scan(&mime, &b64, &idn); err != nil {
		if err == sql.ErrNoRows {
			return nil, "", "", nil
		}
		return nil, "", "", err
	}
	raw, err = base64.StdEncoding.DecodeString(b64)
	return raw, mime, idn, err
}

// DeleteDefendantPhoto removes a photo (audited). The idn it belonged to is
// recorded on the audit row.
func DeleteDefendantPhoto(d *sql.DB, id int64, by string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var idn sql.NullString
	_ = tx.QueryRow(`SELECT idn FROM defendant_photos WHERE photo_id = ?`, id).Scan(&idn)
	res, err := tx.Exec(`DELETE FROM defendant_photos WHERE photo_id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return tx.Commit()
	}
	if err := WriteAudit(tx, AuditEvent{
		User: by, Action: "photo_delete", Table: "defendant_photos", RowID: idn.String,
	}); err != nil {
		return err
	}
	return tx.Commit()
}
