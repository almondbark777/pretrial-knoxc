package ipgeo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDisabledByDefault(t *testing.T) {
	p := New(Config{})
	if p.Enabled() {
		t.Error("provider should be disabled with empty config")
	}
	if got := p.Lookup(context.Background(), "8.8.8.8"); !got.Empty() {
		t.Errorf("disabled lookup returned %+v, want empty", got)
	}
}

func TestPublicIP(t *testing.T) {
	cases := map[string]bool{
		"8.8.8.8":      true,
		"203.0.113.5":  true,
		"10.0.0.4":     false,
		"192.168.1.10": false,
		"127.0.0.1":    false,
		"::1":          false,
		"not-an-ip":    false,
		"":             false,
	}
	for ip, want := range cases {
		if got := publicIP(ip); got != want {
			t.Errorf("publicIP(%q) = %v, want %v", ip, got, want)
		}
	}
}

func TestHTTPProviderMapsFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","city":"Knoxville","regionName":"Tennessee","isp":"Acme Cable"}`))
	}))
	defer srv.Close()

	p := New(Config{Enabled: true, Endpoint: srv.URL + "/{ip}"})
	if !p.Enabled() {
		t.Fatal("provider should be enabled")
	}
	// Private IP is skipped without calling out.
	if got := p.Lookup(context.Background(), "10.1.2.3"); !got.Empty() {
		t.Errorf("private IP should not be looked up, got %+v", got)
	}
	got := p.Lookup(context.Background(), "8.8.8.8")
	if got.City != "Knoxville" || got.Region != "Tennessee" || got.ISP != "Acme Cable" {
		t.Errorf("got %+v, want Knoxville/Tennessee/Acme Cable", got)
	}
}
