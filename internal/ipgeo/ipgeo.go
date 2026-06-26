// Package ipgeo is the IP-geolocation enrichment capability for QR self-check-in:
// turning the server-observed source IP of a submission into a city / region /
// ISP an approving officer can sanity-check ("submitted from a Memphis cable ISP"
// is a tell when the client supposedly reported to a Knoxville lobby).
//
// Like internal/otp it is built but inert by default. New() returns a Disabled
// provider unless the `ip_geo_enabled` config flag is on AND a lookup endpoint is
// configured. With nothing configured the check-in flow sees a provider whose
// Lookup returns an empty result and simply records a blank IP city/region — no
// external call, no dependency, no latency. Turning it on later is an env var +
// a flag flip; no code change.
//
// Privacy/safety stance: it is OFF by default precisely because it sends each
// client's IP to a third party. Private / loopback / unspecified addresses are
// never looked up (they'd resolve to nothing anyway), the call has a tight
// timeout, and any failure degrades to a blank result rather than blocking the
// check-in. Implemented with stdlib net/http — no SDK, no new module dependency.
package ipgeo

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Result is the enrichment for one IP. Zero value (all empty) means "not
// resolved" — the caller stores blanks, which the officer UI renders as "—".
type Result struct {
	City   string
	Region string
	ISP    string
}

// Empty reports whether nothing was resolved.
func (r Result) Empty() bool { return r.City == "" && r.Region == "" && r.ISP == "" }

// Provider is the seam the check-in flow talks to.
type Provider interface {
	// Lookup enriches an IP, best-effort. It never returns an error the caller
	// must handle — a failed/timed-out/disabled lookup is just an empty Result.
	Lookup(ctx context.Context, ip string) Result
	// Enabled reports whether this provider actually calls out.
	Enabled() bool
}

// Config selects and configures the provider. When Enabled is false or Endpoint
// is blank, New returns the Disabled provider.
//
// Endpoint is a URL template containing "{ip}" where the address goes, returning
// JSON. It defaults (when Enabled and left blank) to the free ip-api.com schema,
// but is configurable so the county can point at a paid/self-hosted service or
// supply a token. The response is mapped from common field names (city, region/
// regionName, isp/org), so ip-api.com, ipinfo.io, and similar all work.
type Config struct {
	Enabled  bool
	Endpoint string // e.g. "http://ip-api.com/json/{ip}?fields=city,regionName,isp"
}

const defaultEndpoint = "http://ip-api.com/json/{ip}?fields=status,city,regionName,isp"

// New returns a live HTTP provider only when enabled; otherwise the inert one.
func New(c Config) Provider {
	if !c.Enabled {
		return Disabled{}
	}
	ep := strings.TrimSpace(c.Endpoint)
	if ep == "" {
		ep = defaultEndpoint
	}
	return &httpProvider{endpoint: ep, http: &http.Client{Timeout: 2 * time.Second}}
}

// Disabled is the no-op provider used whenever enrichment is off.
type Disabled struct{}

func (Disabled) Lookup(context.Context, string) Result { return Result{} }
func (Disabled) Enabled() bool                         { return false }

type httpProvider struct {
	endpoint string
	http     *http.Client
}

func (p *httpProvider) Enabled() bool { return true }

func (p *httpProvider) Lookup(ctx context.Context, ip string) Result {
	if !publicIP(ip) {
		return Result{}
	}
	reqURL := strings.ReplaceAll(p.endpoint, "{ip}", url.PathEscape(ip))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return Result{}
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return Result{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}
	}
	// Decode tolerantly against the union of common provider field names.
	var raw struct {
		City       string `json:"city"`
		Region     string `json:"region"`
		RegionName string `json:"regionName"`
		ISP        string `json:"isp"`
		Org        string `json:"org"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Result{}
	}
	region := raw.RegionName
	if region == "" {
		region = raw.Region
	}
	isp := raw.ISP
	if isp == "" {
		isp = raw.Org
	}
	return Result{City: strings.TrimSpace(raw.City), Region: strings.TrimSpace(region), ISP: strings.TrimSpace(isp)}
}

// publicIP reports whether ip is a routable address worth a lookup — skipping
// loopback, link-local, private, and unspecified ranges (office LAN, the tunnel's
// own host, etc.).
func publicIP(ip string) bool {
	addr := net.ParseIP(strings.TrimSpace(ip))
	if addr == nil {
		return false
	}
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsUnspecified() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return false
	}
	return true
}
