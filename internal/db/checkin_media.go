// checkin_media.go is the data layer for the binary attachments on a self
// check-in (migration 012): the selfie the client snaps and, when they sign by
// drawing rather than typing, the signature image. These are stored apart from
// the wide `checkins` row so the approval queue's SELECT stays light; each blob
// is bound to its check-in by a sha256 that's sealed into the row's hash chain,
// so a stored image can't be swapped without detection.
package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// SaveCheckinMedia stores one image (base64-encoded) for a check-in and returns
// the sha256 of the RAW bytes — the value the caller seals into the checkin row
// (selfie_path / signature_data) so the attachment is tamper-evident.
func SaveCheckinMedia(d *sql.DB, checkinID int64, kind, mime string, raw []byte) (string, error) {
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	b64 := base64.StdEncoding.EncodeToString(raw)
	_, err := d.Exec(
		`INSERT INTO checkin_media (checkin_id, kind, mime, sha256, image_b64) VALUES (?, ?, ?, ?, ?)`,
		checkinID, kind, nz(mime), digest, b64)
	if err != nil {
		return "", err
	}
	return digest, nil
}

// GetCheckinMedia returns one attachment (raw bytes + mime + stored digest) for a
// check-in, or (nil,…) when none of that kind exists.
func GetCheckinMedia(d *sql.DB, checkinID int64, kind string) (raw []byte, mime, digest string, err error) {
	var b64 string
	row := d.QueryRow(
		`SELECT mime, sha256, image_b64 FROM checkin_media
		   WHERE checkin_id = ? AND kind = ? ORDER BY media_id DESC LIMIT 1`,
		checkinID, kind)
	if err = row.Scan(&mime, &digest, &b64); err != nil {
		if err == sql.ErrNoRows {
			return nil, "", "", nil
		}
		return nil, "", "", err
	}
	raw, err = base64.StdEncoding.DecodeString(b64)
	return raw, mime, digest, err
}

// VerifyMedia reports whether the stored attachment's bytes still hash to the
// digest sealed in the check-in row — the "this selfie is the one that was
// submitted" check the officer UI and court packet assert on.
func VerifyMedia(raw []byte, sealed string) bool {
	sealed = strings.TrimSpace(strings.TrimPrefix(sealed, "sha256:"))
	if sealed == "" || len(raw) == 0 {
		return false
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]) == sealed
}

// DeviceUsage reports, for a device fingerprint, whether this IDN has checked in
// from it before (seenForIDN) and which OTHER IDNs have used the same device
// (otherIDNs) — the inputs to the "new device" / "shared device" flags. A blank
// device id or the unmatched sentinel "0" yields no signal.
func DeviceUsage(d *sql.DB, deviceID, idn string) (seenForIDN bool, otherIDNs []string) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return false, nil
	}
	rows, err := d.Query(
		`SELECT DISTINCT idn FROM checkins WHERE device_id = ? AND idn <> 0`, deviceID)
	if err != nil {
		return false, nil
	}
	defer rows.Close()
	for rows.Next() {
		var other string
		if rows.Scan(&other) != nil {
			continue
		}
		if other == idn {
			seenForIDN = true
		} else {
			otherIDNs = append(otherIDNs, other)
		}
	}
	return seenForIDN, otherIDNs
}
