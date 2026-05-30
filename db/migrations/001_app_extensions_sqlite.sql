-- ─────────────────────────────────────────────────────────────────────
-- Migration 001 — App-feature extension tables.
-- Adds all tables needed by the modern-UX feature push.
-- Safe to re-run (IF NOT EXISTS guards).
-- ─────────────────────────────────────────────────────────────────────

-- ───── 1. Notes attached to a defendant (free text, by user, with timestamp).

    CREATE TABLE IF NOT EXISTS defendant_notes (
        note_id    INTEGER PRIMARY KEY AUTOINCREMENT,
        idn        INTEGER NOT NULL,
        author     TEXT NULL,
        body       TEXT NOT NULL,
        created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
        CONSTRAINT fk_notes_defendant FOREIGN KEY (idn) REFERENCES defendants(idn)
    );
    CREATE INDEX IF NOT EXISTS idx_notes_idn ON defendant_notes(idn);


-- ───── 2. Tags / labels on a defendant.

    CREATE TABLE IF NOT EXISTS defendant_tags (
        tag_id     INTEGER PRIMARY KEY AUTOINCREMENT,
        idn        INTEGER NOT NULL,
        label      TEXT NOT NULL,
        author     TEXT NULL,
        created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
        CONSTRAINT fk_tags_defendant FOREIGN KEY (idn) REFERENCES defendants(idn)
    );
    CREATE INDEX IF NOT EXISTS idx_tags_idn ON defendant_tags(idn);
    CREATE INDEX IF NOT EXISTS idx_tags_label ON defendant_tags(label);


-- ───── 3. Upcoming court dates per defendant.

    CREATE TABLE IF NOT EXISTS court_dates (
        court_date_id INTEGER PRIMARY KEY AUTOINCREMENT,
        idn           INTEGER NOT NULL,
        court_date    TEXT NOT NULL,
        court         TEXT NULL,
        notes         TEXT NULL,
        author        TEXT NULL,
        created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
        CONSTRAINT fk_courtd_defendant FOREIGN KEY (idn) REFERENCES defendants(idn)
    );
    CREATE INDEX IF NOT EXISTS idx_courtd_idn  ON court_dates(idn);
    CREATE INDEX IF NOT EXISTS idx_courtd_date ON court_dates(court_date);


-- ───── 4. Audit log — track every edit to defendants.

    CREATE TABLE IF NOT EXISTS audit_log (
        audit_id   INTEGER PRIMARY KEY AUTOINCREMENT,
        ts         TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
        user_id    TEXT NULL,
        action     TEXT NOT NULL,
        table_name TEXT NOT NULL,
        row_id     TEXT NULL,
        col_name   TEXT NULL,
        old_value  TEXT NULL,
        new_value  TEXT NULL
    );
    CREATE INDEX IF NOT EXISTS idx_audit_ts    ON audit_log(ts);
    CREATE INDEX IF NOT EXISTS idx_audit_row   ON audit_log(table_name, row_id);


-- ───── 5. Violations.

    CREATE TABLE IF NOT EXISTS violations (
        violation_id  INTEGER PRIMARY KEY AUTOINCREMENT,
        idn           INTEGER NOT NULL,
        violation_date TEXT NOT NULL,
        category      TEXT NULL,    -- 'missed-checkin', 'failed-drug-screen', 'gps-tamper', 'new-arrest', 'other'
        severity      TEXT NULL,     -- 'minor', 'major', 'critical'
        description   TEXT NULL,
        action_taken  TEXT NULL,    -- 'warning', 'da-notified', 'arrest-warrant', 'court-notified'
        officer       TEXT NULL,
        court_notified INTEGER NOT NULL DEFAULT 0,
        da_notified   INTEGER NOT NULL DEFAULT 0,
        created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
        CONSTRAINT fk_violations_defendant FOREIGN KEY (idn) REFERENCES defendants(idn)
    );
    CREATE INDEX IF NOT EXISTS idx_viol_idn  ON violations(idn);
    CREATE INDEX IF NOT EXISTS idx_viol_date ON violations(violation_date);


-- ───── 7. Saved searches (per-user filter combos).

    CREATE TABLE IF NOT EXISTS saved_searches (
        search_id   INTEGER PRIMARY KEY AUTOINCREMENT,
        user_id     TEXT NOT NULL,
        name        TEXT NOT NULL,
        spec        TEXT NOT NULL,   -- JSON: {filters, page, sort}
        page_path   TEXT NULL,        -- which page it applies to
        is_pinned   INTEGER NOT NULL DEFAULT 0,
        created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
    );
    CREATE INDEX IF NOT EXISTS idx_search_user ON saved_searches(user_id);


-- ───── 8. Pinned/starred defendants (per user).

    CREATE TABLE IF NOT EXISTS pinned_defendants (
        pin_id      INTEGER PRIMARY KEY AUTOINCREMENT,
        user_id     TEXT NOT NULL,
        idn         INTEGER NOT NULL,
        created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
        CONSTRAINT fk_pin_defendant FOREIGN KEY (idn) REFERENCES defendants(idn),
        CONSTRAINT uq_pin UNIQUE(user_id, idn)
    );
    CREATE INDEX IF NOT EXISTS idx_pin_user ON pinned_defendants(user_id);


-- ───── 9. User preferences (theme, default landing, etc.).

    CREATE TABLE IF NOT EXISTS user_preferences (
        user_id     TEXT PRIMARY KEY,
        theme       TEXT NULL,        -- 'dark', 'light', 'system'
        default_landing TEXT NULL,   -- e.g. '/my_day.html'
        prefs_json  TEXT NULL,        -- catch-all JSON for future prefs
        updated_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
    );


-- ───── 10. Reminders / per-defendant TODOs (assigned to a user).

    CREATE TABLE IF NOT EXISTS reminders (
        reminder_id INTEGER PRIMARY KEY AUTOINCREMENT,
        idn         INTEGER NULL,               -- nullable: standalone reminders too
        body        TEXT NOT NULL,
        due_date    TEXT NULL,
        assigned_to TEXT NULL,
        created_by  TEXT NULL,
        completed   INTEGER NOT NULL DEFAULT 0,
        completed_at TEXT NULL,
        completed_by TEXT NULL,
        created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
        CONSTRAINT fk_rem_defendant FOREIGN KEY (idn) REFERENCES defendants(idn)
    );
    CREATE INDEX IF NOT EXISTS idx_rem_idn       ON reminders(idn);
    CREATE INDEX IF NOT EXISTS idx_rem_assigned  ON reminders(assigned_to);
    CREATE INDEX IF NOT EXISTS idx_rem_due       ON reminders(due_date);


-- ───── 11. Document attachments metadata (file blobs stored elsewhere).

    CREATE TABLE IF NOT EXISTS defendant_documents (
        doc_id      INTEGER PRIMARY KEY AUTOINCREMENT,
        idn         INTEGER NOT NULL,
        filename    TEXT NOT NULL,
        title       TEXT NULL,
        category    TEXT NULL,        -- 'court-order', 'signed-agreement', 'photo', 'other'
        size_bytes  INTEGER NULL,
        content_type TEXT NULL,
        storage_path TEXT NULL,      -- path in Azure Blob (filled later)
        uploaded_by TEXT NULL,
        uploaded_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
        CONSTRAINT fk_doc_defendant FOREIGN KEY (idn) REFERENCES defendants(idn)
    );
    CREATE INDEX IF NOT EXISTS idx_doc_idn ON defendant_documents(idn);


-- ───── 12. Scheduled check-ins (calendar of upcoming check-ins).

    CREATE TABLE IF NOT EXISTS scheduled_check_ins (
        sched_id        INTEGER PRIMARY KEY AUTOINCREMENT,
        idn             INTEGER NOT NULL,
        scheduled_for   TEXT NOT NULL,
        check_in_type   TEXT NULL,
        officer         TEXT NULL,
        completed_check_in_id INTEGER NULL,    -- FK to check_ins.check_in_id when fulfilled
        created_by      TEXT NULL,
        created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
        CONSTRAINT fk_sched_defendant FOREIGN KEY (idn) REFERENCES defendants(idn)
    );
    CREATE INDEX IF NOT EXISTS idx_sched_idn  ON scheduled_check_ins(idn);
    CREATE INDEX IF NOT EXISTS idx_sched_when ON scheduled_check_ins(scheduled_for);

