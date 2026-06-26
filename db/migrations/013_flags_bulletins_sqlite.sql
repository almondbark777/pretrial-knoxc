-- 013_flags_bulletins_sqlite.sql — manual client flags + the office notice board.
--
-- Mirrored in ensureSchemaSQL (internal/db/admin.go), which is what the running
-- Go app self-provisions at startup. This file is the canonical record (same
-- convention as migrations 001–012).
--
-- client_flags  a manual alert an officer raises on a client (safety/absconding
--               risk, do-not-release, etc.) — a banner on the record and a chip
--               on the roster until another officer clears it. App-owned, audited.
-- bulletins     the office-wide notice board shown on the check-in page: a
--               persistent announcement (unlike the 7-day group chat) every
--               officer sees. High-priority/pinned posts sort to the top.

CREATE TABLE IF NOT EXISTS client_flags (
    flag_id    INTEGER PRIMARY KEY AUTOINCREMENT,
    idn        INTEGER NOT NULL,
    severity   TEXT NOT NULL DEFAULT 'red',   -- red (urgent) | amber (caution)
    reason     TEXT,
    active     INTEGER NOT NULL DEFAULT 1,
    created_by TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    cleared_by TEXT,
    cleared_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_client_flags ON client_flags(idn, active);

CREATE TABLE IF NOT EXISTS bulletins (
    bulletin_id INTEGER PRIMARY KEY AUTOINCREMENT,
    title       TEXT NOT NULL,
    body        TEXT,
    priority    TEXT NOT NULL DEFAULT 'normal', -- normal | high
    pinned      INTEGER NOT NULL DEFAULT 0,
    active      INTEGER NOT NULL DEFAULT 1,
    created_by  TEXT,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_bulletins_active ON bulletins(active, pinned, bulletin_id);
