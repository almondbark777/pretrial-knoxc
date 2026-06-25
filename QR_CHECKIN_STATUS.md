# QR self-check-in — status & remaining work

Self-service check-in: a client scans the lobby QR, fills out the Pre-Trial
Release Reporting Form on their phone, and it lands in an officer approval queue.
Goal is an **evidence-grade** record (defensible in a violation/revocation
hearing) that can eventually replace phone check-ins.

## Design principles
- **Telemetry is split** server-observed (IP, server timestamp, weekly code —
  unspoofable by the form) vs client-supplied (GPS, device fingerprint, locale).
  Labelled as such so we never overclaim the soft signals in court.
- **Append-only + hash-chained.** A check-in is never edited; approve/reject only
  stamp review columns. `sha256(prev_hash + canonical(row))` makes any later
  alteration provable (`VerifyCheckinChain` is the custodian's integrity check).
- **Capabilities built but gated off** behind `checkin_config` flags:
  `sms_otp_enabled` and `background_location_enabled` both default `"0"`.
  Background/continuous location is fenced pending court authorization (Carpenter).
- **Privacy:** with OTP off, the public page does NOT pre-fill from the DB —
  the client types their own info; the server matches name+DOB → IDN at submit.

## Done
- **Phase 1 — data foundation.** Tables `client_contact`, `checkins`,
  `checkin_weekly_codes`, `checkin_config` (migration 011 + mirrored in
  `ensureSchemaSQL`). Data layer in `internal/db/checkins.go` (+ tests).
- **Phase 3 — SMS OTP capability (gated off).** `internal/otp` — Twilio Verify
  via stdlib net/http (zero new deps); inert `Disabled{}` until the flag is on
  AND Twilio creds are set. Sends to the phone ON FILE (proof of possession).
- **Phase 4 — officer approval queue** at `/console/checkins` (nav item + live
  pending-count badge). Per-card 🟢/🟡/🔴 presence badge, flags, telemetry grid,
  citation/arrest answers, approve / reject-with-reason. `internal/handlers/checkins.go`.
- **Phase 2 — public check-in page.** `/checkin` (form + consent + browser
  geolocation/device-fingerprint capture), `/checkin/submit` (server-side
  presence scoring + name/DOB→IDN match + insert), `/checkin/done`.
  `internal/handlers/checkin_public.go`, standalone mobile templates.
  Presence engine: in-geofence→green, outside→red(+matches_home), GPS
  denied→yellow, +stale_code / identity_unmatched / impossible_travel.

## Remaining
- **Phase 5 — court-packet PDF.** Per-check-in tamper-sealed PDF an officer hands
  to a DA (reuse the docx pattern in `internal/emfees/memo.go`).
- **Selfie + liveness capture** — schema cols exist; the public page doesn't snap
  one yet.
- **IP geolocation enrichment** — IP is captured; `ip_city/region/isp` need a
  lookup service to populate.
- **Device binding / new-device flag**, **drawn signature** (currently a
  typed-name e-signature), **rate-limiting** on the public POST (abuse guard).
- **Weekly-code admin UI** — minting/printing the lobby code (data layer exists:
  `CreateWeeklyCode` / `ActiveWeeklyCode`).

## Operational action items (county side, not code)
- Twilio account + **A2P 10DLC registration** (long pole — start early) before
  SMS OTP can be switched on.
- Set the **precise office lat/lng + geofence radius** in `checkin_config`
  (defaults 35.9646, -83.9202, r=150 m are approximate).
- Backfill `client_contact` phones/addresses (geocode home addresses for the
  home-distance comparison).
- Have the **county attorney / DA review the consent wording + data-retention
  policy** before go-live.
- Print the lobby QR pointing at `/checkin?c=<current weekly code>`.
