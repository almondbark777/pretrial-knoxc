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
- **Phase 5 — court-packet PDF.** A tamper-sealed evidence PDF per check-in at
  `/console/checkins/{id}/packet` (button on each queue card). Pure-Go writer
  `internal/pdfgen` (no new deps — same stance as `internal/otp`) + layout in
  `internal/courtpacket`. Server-observed vs client-supplied telemetry labelled
  separately; embeds the selfie + drawn signature as JPEGs and asserts whether
  each still matches the digest sealed in the hash chain; states the chain
  verification result. (`VerifyMedia` + `VerifyCheckinChain`.)
- **Selfie + signature capture.** The public page now snaps a selfie
  (`getUserMedia`, file-input fallback) and offers a drawn-signature pad. Images
  are JPEG, stored in `checkin_media` (migration 012, kept out of the wide
  `checkins` row); each image's sha256 is sealed into the record (selfie via
  `selfie_path`, drawn signature via `signature_data`) so the bytes can't be
  swapped undetected. **Liveness is labelled honestly** as "self-captured" — a
  photo, not a verified-liveness check.
- **Device binding.** `new_device` (client has checked in before but never from
  this handset) and `shared_device` (one handset used for several clients) flags,
  from `db.DeviceUsage`.
- **IP geolocation enrichment.** `internal/ipgeo` — a gated provider mirroring
  `internal/otp`: inert until `ip_geo_enabled='1'` AND `IP_GEO_ENDPOINT` is set.
  Skips private IPs, 2.5s timeout, best-effort blank on failure.
- **Rate-limiting.** In-memory per-IP burst + spacing guard on `/checkin/submit`
  (the one unauthenticated write surface). `internal/handlers/ratelimit.go`.
- **Weekly-code admin UI.** `/console/checkins/codes` mints a fresh lobby code
  (auto-generated, ambiguity-free) and `/console/checkins/poster` prints a
  poster with a **server-generated QR** (pure-Go encoder `internal/qr`, no JS/CDN;
  unit-tested against the QR-spec canonical example and full encode→decode
  round-trips).

## Remaining
- **SMS OTP go-live** (county side — see action items): Twilio + 10DLC, then flip
  `sms_otp_enabled`. Capability already built (`internal/otp`).
- **True liveness** (active challenge / anti-spoof) if ever required — today's
  selfie is labelled "self-captured", not liveness-verified.
- **Background/continuous location** (Phase 6) stays gated pending court
  authorization (Carpenter).

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
