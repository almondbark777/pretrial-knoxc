-- ─────────────────────────────────────────────────────────────────────
-- Migration 001 — App-feature extension tables.
-- Adds all tables needed by the modern-UX feature push.
-- Safe to re-run (IF NOT EXISTS guards).
-- ─────────────────────────────────────────────────────────────────────

-- ───── 1. Notes attached to a defendant (free text, by user, with timestamp).
IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'defendant_notes')
BEGIN
    CREATE TABLE dbo.defendant_notes (
        note_id    BIGINT IDENTITY(1,1) PRIMARY KEY,
        idn        BIGINT NOT NULL,
        author     NVARCHAR(500) NULL,
        body       NVARCHAR(MAX) NOT NULL,
        created_at DATETIME2 NOT NULL CONSTRAINT df_notes_created DEFAULT SYSUTCDATETIME(),
        CONSTRAINT fk_notes_defendant FOREIGN KEY (idn) REFERENCES dbo.defendants(idn)
    );
    CREATE INDEX idx_notes_idn ON dbo.defendant_notes(idn);
END;
GO

-- ───── 2. Tags / labels on a defendant.
IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'defendant_tags')
BEGIN
    CREATE TABLE dbo.defendant_tags (
        tag_id     BIGINT IDENTITY(1,1) PRIMARY KEY,
        idn        BIGINT NOT NULL,
        label      NVARCHAR(100) NOT NULL,
        author     NVARCHAR(500) NULL,
        created_at DATETIME2 NOT NULL CONSTRAINT df_tags_created DEFAULT SYSUTCDATETIME(),
        CONSTRAINT fk_tags_defendant FOREIGN KEY (idn) REFERENCES dbo.defendants(idn)
    );
    CREATE INDEX idx_tags_idn ON dbo.defendant_tags(idn);
    CREATE INDEX idx_tags_label ON dbo.defendant_tags(label);
END;
GO

-- ───── 3. Upcoming court dates per defendant.
IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'court_dates')
BEGIN
    CREATE TABLE dbo.court_dates (
        court_date_id BIGINT IDENTITY(1,1) PRIMARY KEY,
        idn           BIGINT NOT NULL,
        court_date    DATETIME2 NOT NULL,
        court         NVARCHAR(200) NULL,
        notes         NVARCHAR(500) NULL,
        author        NVARCHAR(500) NULL,
        created_at    DATETIME2 NOT NULL CONSTRAINT df_courtd_created DEFAULT SYSUTCDATETIME(),
        CONSTRAINT fk_courtd_defendant FOREIGN KEY (idn) REFERENCES dbo.defendants(idn)
    );
    CREATE INDEX idx_courtd_idn  ON dbo.court_dates(idn);
    CREATE INDEX idx_courtd_date ON dbo.court_dates(court_date);
END;
GO

-- ───── 4. Audit log — track every edit to defendants.
IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'audit_log')
BEGIN
    CREATE TABLE dbo.audit_log (
        audit_id   BIGINT IDENTITY(1,1) PRIMARY KEY,
        ts         DATETIME2 NOT NULL CONSTRAINT df_audit_ts DEFAULT SYSUTCDATETIME(),
        user_id    NVARCHAR(500) NULL,
        action     NVARCHAR(50) NOT NULL,
        table_name NVARCHAR(100) NOT NULL,
        row_id     NVARCHAR(100) NULL,
        col_name   NVARCHAR(100) NULL,
        old_value  NVARCHAR(MAX) NULL,
        new_value  NVARCHAR(MAX) NULL
    );
    CREATE INDEX idx_audit_ts    ON dbo.audit_log(ts);
    CREATE INDEX idx_audit_row   ON dbo.audit_log(table_name, row_id);
END;
GO

-- ───── 5. Violations.
IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'violations')
BEGIN
    CREATE TABLE dbo.violations (
        violation_id  BIGINT IDENTITY(1,1) PRIMARY KEY,
        idn           BIGINT NOT NULL,
        violation_date DATETIME2 NOT NULL,
        category      NVARCHAR(100) NULL,    -- 'missed-checkin', 'failed-drug-screen', 'gps-tamper', 'new-arrest', 'other'
        severity      NVARCHAR(50) NULL,     -- 'minor', 'major', 'critical'
        description   NVARCHAR(MAX) NULL,
        action_taken  NVARCHAR(500) NULL,    -- 'warning', 'da-notified', 'arrest-warrant', 'court-notified'
        officer       NVARCHAR(500) NULL,
        court_notified BIT NOT NULL CONSTRAINT df_viol_courtnotif DEFAULT 0,
        da_notified   BIT NOT NULL CONSTRAINT df_viol_danotif DEFAULT 0,
        created_at    DATETIME2 NOT NULL CONSTRAINT df_viol_created DEFAULT SYSUTCDATETIME(),
        CONSTRAINT fk_violations_defendant FOREIGN KEY (idn) REFERENCES dbo.defendants(idn)
    );
    CREATE INDEX idx_viol_idn  ON dbo.violations(idn);
    CREATE INDEX idx_viol_date ON dbo.violations(violation_date);
END;
GO

-- ───── 7. Saved searches (per-user filter combos).
IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'saved_searches')
BEGIN
    CREATE TABLE dbo.saved_searches (
        search_id   BIGINT IDENTITY(1,1) PRIMARY KEY,
        user_id     NVARCHAR(500) NOT NULL,
        name        NVARCHAR(200) NOT NULL,
        spec        NVARCHAR(MAX) NOT NULL,   -- JSON: {filters, page, sort}
        page_path   NVARCHAR(200) NULL,        -- which page it applies to
        is_pinned   BIT NOT NULL CONSTRAINT df_search_pinned DEFAULT 0,
        created_at  DATETIME2 NOT NULL CONSTRAINT df_search_created DEFAULT SYSUTCDATETIME()
    );
    CREATE INDEX idx_search_user ON dbo.saved_searches(user_id);
END;
GO

-- ───── 8. Pinned/starred defendants (per user).
IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'pinned_defendants')
BEGIN
    CREATE TABLE dbo.pinned_defendants (
        pin_id      BIGINT IDENTITY(1,1) PRIMARY KEY,
        user_id     NVARCHAR(500) NOT NULL,
        idn         BIGINT NOT NULL,
        created_at  DATETIME2 NOT NULL CONSTRAINT df_pin_created DEFAULT SYSUTCDATETIME(),
        CONSTRAINT fk_pin_defendant FOREIGN KEY (idn) REFERENCES dbo.defendants(idn),
        CONSTRAINT uq_pin UNIQUE(user_id, idn)
    );
    CREATE INDEX idx_pin_user ON dbo.pinned_defendants(user_id);
END;
GO

-- ───── 9. User preferences (theme, default landing, etc.).
IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'user_preferences')
BEGIN
    CREATE TABLE dbo.user_preferences (
        user_id     NVARCHAR(500) PRIMARY KEY,
        theme       NVARCHAR(20) NULL,        -- 'dark', 'light', 'system'
        default_landing NVARCHAR(200) NULL,   -- e.g. '/my_day.html'
        prefs_json  NVARCHAR(MAX) NULL,        -- catch-all JSON for future prefs
        updated_at  DATETIME2 NOT NULL CONSTRAINT df_prefs_updated DEFAULT SYSUTCDATETIME()
    );
END;
GO

-- ───── 10. Reminders / per-defendant TODOs (assigned to a user).
IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'reminders')
BEGIN
    CREATE TABLE dbo.reminders (
        reminder_id BIGINT IDENTITY(1,1) PRIMARY KEY,
        idn         BIGINT NULL,               -- nullable: standalone reminders too
        body        NVARCHAR(MAX) NOT NULL,
        due_date    DATETIME2 NULL,
        assigned_to NVARCHAR(500) NULL,
        created_by  NVARCHAR(500) NULL,
        completed   BIT NOT NULL CONSTRAINT df_rem_completed DEFAULT 0,
        completed_at DATETIME2 NULL,
        completed_by NVARCHAR(500) NULL,
        created_at  DATETIME2 NOT NULL CONSTRAINT df_rem_created DEFAULT SYSUTCDATETIME(),
        CONSTRAINT fk_rem_defendant FOREIGN KEY (idn) REFERENCES dbo.defendants(idn)
    );
    CREATE INDEX idx_rem_idn       ON dbo.reminders(idn);
    CREATE INDEX idx_rem_assigned  ON dbo.reminders(assigned_to);
    CREATE INDEX idx_rem_due       ON dbo.reminders(due_date);
END;
GO

-- ───── 11. Document attachments metadata (file blobs stored elsewhere).
IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'defendant_documents')
BEGIN
    CREATE TABLE dbo.defendant_documents (
        doc_id      BIGINT IDENTITY(1,1) PRIMARY KEY,
        idn         BIGINT NOT NULL,
        filename    NVARCHAR(500) NOT NULL,
        title       NVARCHAR(500) NULL,
        category    NVARCHAR(100) NULL,        -- 'court-order', 'signed-agreement', 'photo', 'other'
        size_bytes  BIGINT NULL,
        content_type NVARCHAR(100) NULL,
        storage_path NVARCHAR(1000) NULL,      -- path in Azure Blob (filled later)
        uploaded_by NVARCHAR(500) NULL,
        uploaded_at DATETIME2 NOT NULL CONSTRAINT df_doc_uploaded DEFAULT SYSUTCDATETIME(),
        CONSTRAINT fk_doc_defendant FOREIGN KEY (idn) REFERENCES dbo.defendants(idn)
    );
    CREATE INDEX idx_doc_idn ON dbo.defendant_documents(idn);
END;
GO

-- ───── 12. Scheduled check-ins (calendar of upcoming check-ins).
IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'scheduled_check_ins')
BEGIN
    CREATE TABLE dbo.scheduled_check_ins (
        sched_id        BIGINT IDENTITY(1,1) PRIMARY KEY,
        idn             BIGINT NOT NULL,
        scheduled_for   DATETIME2 NOT NULL,
        check_in_type   NVARCHAR(100) NULL,
        officer         NVARCHAR(500) NULL,
        completed_check_in_id BIGINT NULL,    -- FK to check_ins.check_in_id when fulfilled
        created_by      NVARCHAR(500) NULL,
        created_at      DATETIME2 NOT NULL CONSTRAINT df_sched_created DEFAULT SYSUTCDATETIME(),
        CONSTRAINT fk_sched_defendant FOREIGN KEY (idn) REFERENCES dbo.defendants(idn)
    );
    CREATE INDEX idx_sched_idn  ON dbo.scheduled_check_ins(idn);
    CREATE INDEX idx_sched_when ON dbo.scheduled_check_ins(scheduled_for);
END;
GO
