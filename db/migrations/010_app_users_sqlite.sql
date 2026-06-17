-- 010_app_users_sqlite.sql — in-app roles & permissions.
--
-- Moves the allow-list + privilege tier out of static env vars (ALLOWED_EMAILS /
-- SUPERVISOR_EMAILS, frozen at startup) into an app-managed table an Admin can edit
-- from /console/admin — no redeploy to hand out or revoke roles.
--
-- Three hierarchical roles: officer < supervisor < admin.
--   officer     read + log notes/check-ins/payments/court/violations/etc.
--   supervisor  + deletes/restores, field overrides, fee waivers, caseload, import.
--   admin       + manage users & roles (this table).
--
-- Auth treats this table as the source of truth once seeded; the env lists are used
-- only to seed an empty table (db.SeedUsersIfEmpty, called from main) and as a
-- fail-safe fallback if the DB lookup ever fails. A hardcoded break-glass admin
-- (alexander.bentley@knoxsheriff.org, or ADMIN_EMAILS) is ALWAYS admin so no UI
-- mistake can lock the owner out. Every change is audited (user_role_set/user_remove).
--
-- Idempotent (IF NOT EXISTS); the Go server self-provisions this at startup
-- (internal/db/admin.go EnsureSchema).

CREATE TABLE IF NOT EXISTS app_users (
    email      TEXT PRIMARY KEY,                    -- lower-cased
    role       TEXT NOT NULL DEFAULT 'officer',     -- officer | supervisor | admin
    added_by   TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
