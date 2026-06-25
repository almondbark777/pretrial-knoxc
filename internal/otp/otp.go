// Package otp is the SMS one-time-code capability for QR self-check-in. It is a
// proof-of-possession factor: the code is sent to the phone ALREADY ON FILE for
// the client (never a number typed at the kiosk), so entering it proves the
// client is holding the registered handset.
//
// The capability is built but inert by default. New() returns a Disabled
// provider unless the `sms_otp_enabled` config flag is on AND Twilio Verify
// credentials are present — so with nothing configured, the check-in flow sees
// a provider whose calls return ErrDisabled and simply skips the OTP step.
// Turning it on later is env vars + a flag flip; no code change.
//
// Twilio Verify is used over raw SMS because it generates the code, sets expiry,
// caps attempts, and rate-limits server-side — we never store the code. The
// implementation is plain net/http (no SDK, no new module dependency), matching
// the project's single-binary / minimal-deps stance.
package otp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrDisabled is returned by every call on the inert provider, so a handler can
// treat "OTP turned off" and "OTP failed" along the same branch.
var ErrDisabled = errors.New("otp: SMS verification is disabled")

// Provider is the seam the check-in flow talks to. Start sends a code; Check
// validates one. Both take an E.164 phone ("+18655551234").
type Provider interface {
	Start(ctx context.Context, phoneE164 string) error
	Check(ctx context.Context, phoneE164, code string) (bool, error)
	// Enabled reports whether this provider actually sends codes (false for the
	// Disabled provider) — lets the handler decide to show/skip the OTP step.
	Enabled() bool
	// Name identifies the provider in logs/diagnostics.
	Name() string
}

// Config selects and configures the provider. When Enabled is false or any
// Twilio field is blank, New returns the Disabled provider.
type Config struct {
	Enabled    bool
	AccountSID string // TWILIO_ACCOUNT_SID
	AuthToken  string // TWILIO_AUTH_TOKEN
	ServiceSID string // TWILIO_VERIFY_SERVICE_SID (the Verify Service, "VA...")
}

// New returns a live Twilio provider only when fully configured and enabled;
// otherwise an inert Disabled provider. This is the single gate for the whole
// capability.
func New(c Config) Provider {
	if !c.Enabled || c.AccountSID == "" || c.AuthToken == "" || c.ServiceSID == "" {
		return Disabled{}
	}
	return &twilioVerify{
		accountSID: c.AccountSID,
		authToken:  c.AuthToken,
		serviceSID: c.ServiceSID,
		http:       &http.Client{Timeout: 10 * time.Second},
	}
}

// Disabled is the no-op provider used whenever SMS OTP is off. It sends nothing
// and verifies nothing.
type Disabled struct{}

func (Disabled) Start(context.Context, string) error                 { return ErrDisabled }
func (Disabled) Check(context.Context, string, string) (bool, error) { return false, ErrDisabled }
func (Disabled) Enabled() bool                                       { return false }
func (Disabled) Name() string                                        { return "disabled" }

// twilioVerify calls the Twilio Verify v2 REST API. We never persist the code —
// Twilio holds it and enforces expiry/attempts/rate-limits.
type twilioVerify struct {
	accountSID string
	authToken  string
	serviceSID string
	http       *http.Client
}

const twilioVerifyBase = "https://verify.twilio.com/v2/Services/"

func (t *twilioVerify) Start(ctx context.Context, phoneE164 string) error {
	phoneE164 = strings.TrimSpace(phoneE164)
	if phoneE164 == "" {
		return errors.New("otp: empty phone")
	}
	form := url.Values{"To": {phoneE164}, "Channel": {"sms"}}
	var out struct {
		Status string `json:"status"`
		Code   int    `json:"code"`
		Msg    string `json:"message"`
	}
	if err := t.post(ctx, t.serviceSID+"/Verifications", form, &out); err != nil {
		return err
	}
	if out.Code != 0 {
		return fmt.Errorf("otp: twilio start failed (%d): %s", out.Code, out.Msg)
	}
	return nil
}

func (t *twilioVerify) Check(ctx context.Context, phoneE164, code string) (bool, error) {
	phoneE164, code = strings.TrimSpace(phoneE164), strings.TrimSpace(code)
	if phoneE164 == "" || code == "" {
		return false, errors.New("otp: empty phone or code")
	}
	form := url.Values{"To": {phoneE164}, "Code": {code}}
	var out struct {
		Status string `json:"status"` // "approved" on success
		Code   int    `json:"code"`
		Msg    string `json:"message"`
	}
	if err := t.post(ctx, t.serviceSID+"/VerificationCheck", form, &out); err != nil {
		return false, err
	}
	if out.Code != 0 && out.Status == "" {
		return false, fmt.Errorf("otp: twilio check failed (%d): %s", out.Code, out.Msg)
	}
	return out.Status == "approved", nil
}

func (t *twilioVerify) Enabled() bool { return true }
func (t *twilioVerify) Name() string  { return "twilio-verify" }

func (t *twilioVerify) post(ctx context.Context, path string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, twilioVerifyBase+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(t.accountSID, t.authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// Mask renders a phone as "•••-•••-1234" for display and audit — we log that a
// code was sent, never the full number alongside the rest of the record.
func Mask(phoneE164 string) string {
	digits := make([]rune, 0, len(phoneE164))
	for _, r := range phoneE164 {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	if len(digits) < 4 {
		return "•••"
	}
	return "•••-•••-" + string(digits[len(digits)-4:])
}
