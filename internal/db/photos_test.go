package db

import (
	"bytes"
	"testing"
)

// Defendant/victim photos (problem report #10): save → list → fetch bytes →
// delete round-trips, and kind is normalized.
func TestDefendantPhotosFlow(t *testing.T) {
	d := openEnsured(t)
	idn := "660001"
	raw := []byte("\xff\xd8\xff\xe0fakejpegbytes")
	if err := SaveDefendantPhoto(d, idn, "Victim", "image/jpeg", "booking", raw, "tester"); err != nil {
		t.Fatalf("save: %v", err)
	}
	list, err := ListDefendantPhotos(d, idn)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %d (err %v), want 1", len(list), err)
	}
	if list[0].Kind != "victim" { // normalized from "Victim"
		t.Fatalf("kind = %q, want victim", list[0].Kind)
	}
	got, mime, owner, err := GetDefendantPhoto(d, list[0].ID)
	if err != nil || !bytes.Equal(got, raw) || mime != "image/jpeg" || owner != idn {
		t.Fatalf("get = %q/%q/%q err=%v, want roundtrip", got, mime, owner, err)
	}
	if err := DeleteDefendantPhoto(d, list[0].ID, "tester"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ = ListDefendantPhotos(d, idn)
	if len(list) != 0 {
		t.Fatalf("after delete = %d, want 0", len(list))
	}
}
