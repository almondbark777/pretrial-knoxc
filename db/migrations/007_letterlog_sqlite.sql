-- 007_letterlog_sqlite.sql — per-client letter-generation history.
--
-- Every past-due EM-fee memo the site generates (single download or batch zip)
-- is recorded here, so the report can show when each client last had a letter
-- and officers can decide who belongs in the next print run. Rows are written
-- by the app only (never the importer), purged with the person on a
-- whole-person delete, and mirrored in EnsureSchema for self-provisioning.

CREATE TABLE IF NOT EXISTS letter_log (
    letter_id    INTEGER PRIMARY KEY AUTOINCREMENT,
    idn          TEXT NOT NULL,
    case_number  TEXT NULL,
    letter_type  TEXT NOT NULL DEFAULT 'em_fees',
    detail       TEXT NULL,           -- e.g. "behind $640.00 · open"
    generated_by TEXT NULL,           -- officer email
    created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_letter_idn ON letter_log(idn);
