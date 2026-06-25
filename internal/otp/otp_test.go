package otp

import (
	"context"
	"errors"
	"testing"
)

// The capability must stay inert until it is BOTH enabled and fully credentialed
// — that's the whole "built but off for now" guarantee.
func TestNewReturnsDisabledUnlessFullyConfigured(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool // want Enabled()
	}{
		{"zero value", Config{}, false},
		{"enabled but no creds", Config{Enabled: true}, false},
		{"creds but disabled", Config{AccountSID: "AC", AuthToken: "tok", ServiceSID: "VA"}, false},
		{"enabled, missing service sid", Config{Enabled: true, AccountSID: "AC", AuthToken: "tok"}, false},
		{"fully configured", Config{Enabled: true, AccountSID: "AC", AuthToken: "tok", ServiceSID: "VA"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := New(c.cfg).Enabled(); got != c.want {
				t.Fatalf("Enabled() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestDisabledProviderErrsAndNeverApproves(t *testing.T) {
	p := New(Config{}) // disabled
	if err := p.Start(context.Background(), "+18655551234"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Start err = %v, want ErrDisabled", err)
	}
	ok, err := p.Check(context.Background(), "+18655551234", "123456")
	if ok || !errors.Is(err, ErrDisabled) {
		t.Fatalf("Check = (%v, %v), want (false, ErrDisabled)", ok, err)
	}
}

func TestMask(t *testing.T) {
	cases := map[string]string{
		"+18655551234": "•••-•••-1234",
		"865-555-9999": "•••-•••-9999",
		"12":           "•••",
		"":             "•••",
	}
	for in, want := range cases {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}
