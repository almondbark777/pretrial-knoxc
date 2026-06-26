package db

import "testing"

func TestCheckinMediaRoundTrip(t *testing.T) {
	d := openEnsured(t)
	id, _, err := InsertCheckin(d, sampleCheckin("100200300", "Alpha Tester", "green"))
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3, 4, 5}

	digest, err := SaveCheckinMedia(d, id, "selfie", "image/jpeg", raw)
	if err != nil {
		t.Fatal(err)
	}
	if digest == "" {
		t.Fatal("expected a digest")
	}
	got, mime, storedDigest, err := GetCheckinMedia(d, id, "selfie")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Errorf("round-trip mismatch: got %v", got)
	}
	if mime != "image/jpeg" || storedDigest != digest {
		t.Errorf("mime/digest mismatch: %q %q", mime, storedDigest)
	}
	// Integrity: the stored bytes verify against the sealed digest; tampered don't.
	if !VerifyMedia(got, "sha256:"+digest) {
		t.Error("VerifyMedia should pass for the original bytes")
	}
	if VerifyMedia([]byte{0xFF, 0xD8, 0x00}, "sha256:"+digest) {
		t.Error("VerifyMedia should fail for altered bytes")
	}
	// Missing kind returns nothing without error.
	if raw, _, _, err := GetCheckinMedia(d, id, "signature"); err != nil || raw != nil {
		t.Errorf("missing media should be (nil,nil): %v %v", raw, err)
	}
}

func TestDeviceUsage(t *testing.T) {
	d := openEnsured(t)
	// Two check-ins on the same device for IDN 111; one on the same device for 222.
	for i := 0; i < 2; i++ {
		c := sampleCheckin("111", "First Client", "green")
		c.DeviceID = "devAAA"
		if _, _, err := InsertCheckin(d, c); err != nil {
			t.Fatal(err)
		}
	}
	c := sampleCheckin("222", "Second Client", "green")
	c.DeviceID = "devAAA"
	if _, _, err := InsertCheckin(d, c); err != nil {
		t.Fatal(err)
	}

	seen, others := DeviceUsage(d, "devAAA", "111")
	if !seen {
		t.Error("device should be seen for IDN 111")
	}
	if len(others) != 1 || others[0] != "222" {
		t.Errorf("expected other IDN [222], got %v", others)
	}
	// A never-seen device on a fresh IDN: not seen, no others.
	if seen, others := DeviceUsage(d, "devZZZ", "333"); seen || len(others) != 0 {
		t.Errorf("unseen device: seen=%v others=%v", seen, others)
	}
	// Blank device id yields no signal.
	if seen, others := DeviceUsage(d, "", "111"); seen || others != nil {
		t.Errorf("blank device should give no signal: %v %v", seen, others)
	}
}

func TestListWeeklyCodes(t *testing.T) {
	d := openEnsured(t)
	if _, err := CreateWeeklyCode(d, "AAA-1", "Week 1", "2026-06-01", "2026-06-07", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateWeeklyCode(d, "BBB-2", "Week 2", "2026-06-08", "2026-06-14", "tester"); err != nil {
		t.Fatal(err)
	}
	codes, err := ListWeeklyCodes(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 2 {
		t.Fatalf("expected 2 codes, got %d", len(codes))
	}
	// The newest (and only active) sorts first.
	if !codes[0].Active || codes[0].Code != "BBB-2" {
		t.Errorf("active code should sort first: %+v", codes[0])
	}
	if codes[1].Active {
		t.Error("the prior code should be deactivated")
	}
}
