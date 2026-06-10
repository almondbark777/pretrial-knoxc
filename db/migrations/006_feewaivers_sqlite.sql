-- 006_feewaivers_sqlite.sql — supervisor GPS fee waivers (SQLite).
--
-- Backs the console record's "Waive GPS fees" action. The vendor's GPS notes
-- carry historical waivers as free text (matched by compute.IsFeesWaived /
-- the tracker's isFeesWaived regex); this table is the app's own way to grant
-- one. The server splices a matching marker into gp_notes at read time
-- (BuildClients + the tracker feed), so IsFeesWaived stays the single source
-- of truth and every view lights up without a second flag. App-owned
-- extension table: never touched by the importer, purged on whole-person
-- delete, every write audited.
--
-- Idempotent (IF NOT EXISTS); the Go server also self-provisions this at
-- startup via EnsureSchema (internal/db/admin.go).

CREATE TABLE IF NOT EXISTS fee_waivers (
    waiver_id   INTEGER PRIMARY KEY AUTOINCREMENT,
    idn         TEXT NOT NULL,
    reason      TEXT NULL,
    waived_by   TEXT NULL,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_waiver_idn UNIQUE(idn)
);
