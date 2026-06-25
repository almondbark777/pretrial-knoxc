-- 011_checkins_sqlite.sql — QR self-check-in foundation.
--
-- This DDL is mirrored in ensureSchemaSQL (internal/db/admin.go), which is the
-- table the running Go app self-provisions from at startup. This file is the
-- canonical record (same convention as migrations 001–010).
--
-- Four tables:
--   client_contact        structured phone + home address per client. Phone is
--                         the SMS-OTP destination; home_lat/lng backs the
--                         "checked in from their own house" comparison. Neither
--                         existed before — phone/address lived only in notes.
--   checkin_weekly_codes  rotating code the lobby QR encodes (provenance, not a
--                         hard gate).
--   checkins              append-only, tamper-evident submission record that,
--                         once approved, replaces the paper reporting form.
--                         Telemetry is split server-observed vs client-supplied.
--                         prev_hash → record_hash is a sha256 chain.
--   checkin_config        key/value flags: office geofence, consent version,
--                         and background_location_enabled (default off — the
--                         capability is built but fenced behind court authorization).

CREATE TABLE IF NOT EXISTS client_contact (
    idn               INTEGER PRIMARY KEY,
    phone_e164        TEXT,
    phone_verified_at TEXT,
    address_line1     TEXT,
    address_line2     TEXT,
    city              TEXT,
    state             TEXT,
    zip               TEXT,
    home_lat          REAL,
    home_lng          REAL,
    updated_by        TEXT,
    updated_at        TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS checkin_weekly_codes (
    code_id    INTEGER PRIMARY KEY AUTOINCREMENT,
    code       TEXT NOT NULL UNIQUE,
    label      TEXT,
    valid_from TEXT NOT NULL,
    valid_to   TEXT NOT NULL,
    active     INTEGER NOT NULL DEFAULT 1,
    created_by TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_weekcode_active ON checkin_weekly_codes(active, valid_to);

CREATE TABLE IF NOT EXISTS checkins (
    checkin_id        INTEGER PRIMARY KEY AUTOINCREMENT,
    idn               INTEGER NOT NULL,
    status            TEXT NOT NULL DEFAULT 'pending',
    report_type       TEXT,
    client_name       TEXT,
    phone             TEXT,
    address_line1     TEXT,
    address_line2     TEXT,
    city              TEXT,
    state             TEXT,
    zip               TEXT,
    employment_status TEXT,
    employer          TEXT,
    unemployed_length TEXT,
    citation_since    INTEGER NOT NULL DEFAULT 0,
    citation_date     TEXT,
    arrested_since    INTEGER NOT NULL DEFAULT 0,
    arrested_date     TEXT,
    next_court_date   TEXT,
    signature_kind    TEXT,
    signature_data    TEXT,
    consent_version   TEXT,
    consent_text      TEXT,
    consent_at        TEXT,
    server_ts         TEXT NOT NULL,
    src_ip            TEXT,
    ip_city           TEXT,
    ip_region         TEXT,
    ip_isp            TEXT,
    week_code_id      INTEGER,
    week_code_valid   INTEGER NOT NULL DEFAULT 1,
    client_ts         TEXT,
    gps_lat           REAL,
    gps_lng           REAL,
    gps_accuracy_m    REAL,
    gps_perm          TEXT,
    timezone          TEXT,
    locale            TEXT,
    user_agent        TEXT,
    device_id         TEXT,
    otp_phone_mask    TEXT,
    otp_verified_at   TEXT,
    selfie_path       TEXT,
    selfie_liveness   TEXT,
    dist_office_m     REAL,
    dist_home_m       REAL,
    presence_badge    TEXT,
    flags             TEXT,
    prev_hash         TEXT,
    record_hash       TEXT,
    approved_by       TEXT,
    approved_at       TEXT,
    reject_reason     TEXT,
    created_at        TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_checkins_idn ON checkins(idn);
CREATE INDEX IF NOT EXISTS idx_checkins_status ON checkins(status, created_at);

CREATE TABLE IF NOT EXISTS checkin_config (
    key   TEXT PRIMARY KEY,
    value TEXT
);
