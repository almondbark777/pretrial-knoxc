-- 005_drugscreens_sqlite.sql — drug-screen log (SQLite).
--
-- The "drug-screen logging (table + CRUD)" roadmap item from the old Python
-- app. Officers record each screen (date, test type, result, substances when
-- positive, notes) on the client profile; failed screens are typically also
-- recorded as a violation (category failed-drug-screen) — this table is the
-- per-test log behind that. App-owned extension table: never touched by the
-- importer, purged on whole-person delete, every write audited.
--
-- Idempotent (IF NOT EXISTS); the Go server also self-provisions this at
-- startup via EnsureSchema (internal/db/admin.go).

CREATE TABLE IF NOT EXISTS drug_screens (
    screen_id   INTEGER PRIMARY KEY AUTOINCREMENT,
    idn         INTEGER NOT NULL,
    screen_date TEXT NOT NULL,
    test_type   TEXT NULL,      -- urine / oral swab / hair / breath / other
    result      TEXT NULL,      -- negative / positive / diluted / refused / pending
    substances  TEXT NULL,      -- what it was positive for (free text)
    notes       TEXT NULL,
    officer     TEXT NULL,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_screen_idn ON drug_screens(idn);
