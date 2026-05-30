-- ─────────────────────────────────────────────────────────────────────
-- Migration 002 — Grant write permissions to app_reader.
--
-- Background: production App Service connects as `app_reader`, originally
-- created with db_datareader (SELECT-only). The feature push adds many
-- write endpoints (notes, tags, court dates, violations, reminders, inline
-- edit, pins, prefs, audit log) that fail under read-only.
--
-- This grants INSERT/UPDATE/DELETE on the specific tables the app writes
-- to. We deliberately do NOT grant on raw_* tables (those are source-of-
-- truth), nor on the v_defendant_summary view (read-only by design).
--
-- Safe to re-run (GRANT is idempotent in SQL Server).
-- ─────────────────────────────────────────────────────────────────────

-- Existing normalized tables the app now writes to:
GRANT INSERT, UPDATE, DELETE ON dbo.defendants            TO app_reader;
GRANT INSERT, UPDATE, DELETE ON dbo.cases                 TO app_reader;
GRANT INSERT, UPDATE, DELETE ON dbo.check_ins             TO app_reader;
GRANT INSERT, UPDATE, DELETE ON dbo.payments              TO app_reader;
GRANT INSERT, UPDATE, DELETE ON dbo.gps_events            TO app_reader;

-- New tables added in migration 001:
GRANT INSERT, UPDATE, DELETE ON dbo.defendant_notes       TO app_reader;
GRANT INSERT, UPDATE, DELETE ON dbo.defendant_tags        TO app_reader;
GRANT INSERT, UPDATE, DELETE ON dbo.court_dates           TO app_reader;
GRANT INSERT, UPDATE, DELETE ON dbo.audit_log             TO app_reader;
GRANT INSERT, UPDATE, DELETE ON dbo.violations            TO app_reader;
GRANT INSERT, UPDATE, DELETE ON dbo.saved_searches        TO app_reader;
GRANT INSERT, UPDATE, DELETE ON dbo.pinned_defendants     TO app_reader;
GRANT INSERT, UPDATE, DELETE ON dbo.user_preferences      TO app_reader;
GRANT INSERT, UPDATE, DELETE ON dbo.reminders             TO app_reader;
GRANT INSERT, UPDATE, DELETE ON dbo.defendant_documents   TO app_reader;
GRANT INSERT, UPDATE, DELETE ON dbo.scheduled_check_ins   TO app_reader;

-- Drug screens — table still exists but unused by the app right now;
-- no harm in granting in case it's wired back later.
GRANT INSERT, UPDATE, DELETE ON dbo.drug_screens          TO app_reader;
GO
