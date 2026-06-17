-- 009_caseload_sqlite.sql — A–Z caseload assignment by last-name initial.
--
-- Supervisors divide the caseload by the first letter of the defendant's last
-- name: each letter A–Z is owned by exactly one officer (PRIMARY KEY on letter).
-- When a new referral is created with the officer left on "Auto — by last name",
-- the app assigns the client to whoever owns that letter (see
-- internal/db/caseload.go OfficerForLastName + the AddDefendant handler).
--
-- Idempotent (IF NOT EXISTS); the Go server also self-provisions this at startup
-- (internal/db/admin.go EnsureSchema).

CREATE TABLE IF NOT EXISTS caseload_letters (
    letter     TEXT PRIMARY KEY,                       -- 'A'..'Z'
    officer    TEXT NOT NULL,                          -- supervising officer display name
    author     TEXT,                                   -- who set it (audit convenience)
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
