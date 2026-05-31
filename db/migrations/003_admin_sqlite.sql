-- ─────────────────────────────────────────────────────────────────────
-- Migration 003 — Admin & data-entry layer (SQLite).
--
-- Adds the two tables the supervisor write/correction surface needs:
--   * deleted_idns — tombstones. A row suppresses a person (case_number NULL)
--     or a single case (case_number = a case token) from EVERY read view via
--     internal/db/db.go BuildClients. Because the importer never touches this
--     extension table, a tombstone survives the Sunday full reload of
--     raw_blue_book — the person stays gone across imports (importer-proof).
--   * overrides — supervisor typo-fixes to imported fields, keyed by
--     (idn, field). Applied in BuildClients AFTER the raw read, so an obvious
--     typo (wrong pretrial level / referral date) can be corrected immediately
--     without SQL. Clearly labelled "override (app)" on the profile.
--
-- Style mirrors 001_app_extensions_sqlite.sql: native SQLite, IF NOT EXISTS
-- guards so it is safe to re-run (the Go server runs it at startup; see
-- internal/db.EnsureSchema). The app NEVER writes to raw_* tables except the
-- IMPORTER_RETIRED physical-delete path (Brief Part 5.4).
-- ─────────────────────────────────────────────────────────────────────

-- ───── 1. Tombstones — durable suppression of a person or a single case.

    CREATE TABLE IF NOT EXISTS deleted_idns (
        tombstone_id INTEGER PRIMARY KEY AUTOINCREMENT,
        idn          TEXT NOT NULL,
        case_number  TEXT NULL,        -- NULL = whole person; a case token = one case
        deleted_by   TEXT NULL,
        deleted_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
        reason       TEXT NULL
    );
    CREATE INDEX IF NOT EXISTS idx_deleted_idns_idn ON deleted_idns(idn);
    -- One tombstone per target. IFNULL(...,'') folds the whole-person row
    -- (case_number NULL) into a single key so a re-delete is INSERT OR IGNORE.
    CREATE UNIQUE INDEX IF NOT EXISTS uq_deleted_idns
        ON deleted_idns(idn, IFNULL(case_number, ''));


-- ───── 2. Field overrides — audited typo-fixes to imported fields.

    CREATE TABLE IF NOT EXISTS overrides (
        override_id INTEGER PRIMARY KEY AUTOINCREMENT,
        idn         TEXT NOT NULL,
        field       TEXT NOT NULL,     -- raw_blue_book column key (pretrial_level, referral_date, …)
        value       TEXT NOT NULL,
        author      TEXT NULL,
        created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
    );
    CREATE INDEX IF NOT EXISTS idx_overrides_idn ON overrides(idn);
    CREATE UNIQUE INDEX IF NOT EXISTS uq_overrides ON overrides(idn, field);
