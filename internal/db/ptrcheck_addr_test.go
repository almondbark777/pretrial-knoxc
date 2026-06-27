package db

import "testing"

// PTR-check "addressed" marks (problem report #9): set is idempotent, load
// reflects it, clear removes it.
func TestPtrCheckAddressedFlow(t *testing.T) {
	d := openEnsured(t)
	idn := "770001"
	if err := SetPtrCheckAddressed(d, idn, "tester"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := SetPtrCheckAddressed(d, idn, "tester"); err != nil {
		t.Fatalf("set (idempotent): %v", err)
	}
	set, err := LoadPtrCheckAddressed(d)
	if err != nil || !set[idn] {
		t.Fatalf("load = %v err=%v, want idn present", set, err)
	}
	if err := ClearPtrCheckAddressed(d, idn, "tester"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	set, _ = LoadPtrCheckAddressed(d)
	if set[idn] {
		t.Fatal("idn should be cleared")
	}
}
