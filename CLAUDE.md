# CLAUDE.md

Project memory for the `pretrial-knoxc` repo. Read this before touching anything.

---

## What this project is

A web app for the Knox County Sheriff's Office Pre-Trial Services division.
Officers look up defendants, check-in history, payment history, and GPS status.
Replaces a SharePoint-list-plus-Excel workflow. Self-hosted on a Linux server
called ptr1 inside the office, exposed via Cloudflare Tunnel.

**Current status: pre-production. Not yet in active use. Cleaning up and
rewriting before first real test.**

---

## Infrastructure (self-hosted, no cloud services except tunnel)

- **Server:** ptr1 — Linux (Ubuntu/Debian) box at the office
- **App:** runs as `ptr-webapp` systemd service, bound to `127.0.0.1:8000`
- **Database:** SQLite at `/opt/ptr-knoxc/db/pretrial_release.db`
- **Data sync:** `ptr-import.timer` systemd timer runs daily, pulls CSV exports
  from Power Automate (SharePoint -> Gmail -> IMAP -> SQLite)
- **Tunnel:** `cloudflared` systemd service, routes `https://ptr.<domain>` -> `http://127.0.0.1:8000`
- **Outer gate:** Cloudflare Zero Trust Access — email must be on allowlist, verified by one-time code
- **Inner gate:** App login — `@knoxsheriff.org` email + shared `APP_PASSWORD`

Access flow:
```
Browser -> Cloudflare Access (allowlist email + emailed one-time code)
        -> App login (email + APP_PASSWORD)
        -> Inside the app
```

---

## Planned rewrite: Python/FastAPI -> Go

**The current Python/FastAPI app is being rewritten in Go before production use.**

Reasons:
- Current codebase has a T-SQL -> SQLite runtime translation shim (sqlglot)
  that adds overhead and is hard to debug. All queries need to be rewritten as
  native SQLite anyway.
- Go: single binary deploy, no dependencies on ptr1, much faster, easy to read.
- Deploy story: `go build`, `scp` binary to ptr1, `systemctl restart ptr-webapp`.

### Go rewrite goals
- Native SQLite queries (no T-SQL, no sqlglot shim)
- `html/template` for server-rendered pages
- Single binary — no venv, no pip, no Python version management
- Same FastAPI routes and JSON API surface (so JS in templates stays the same)
- Same auth flow (session cookie + HTTP Basic fallback)
- Same systemd service and cloudflared setup unchanged
- Database renamed from `kh222.db` to `pretrial_release.db`

### Recommended Go packages
- `github.com/mattn/go-sqlite3` or `modernc.org/sqlite` (pure Go, no CGO) for SQLite
- `github.com/gorilla/sessions` or built-in `net/http` sessions for auth
- Standard library `html/template` for templates
- Standard library `net/http` for the server (or `github.com/go-chi/chi` for routing)

---

## Database

**File:** `/opt/ptr-knoxc/db/pretrial_release.db` (was `kh222.db`)

Source data refreshed daily by the import timer:
- `raw_blue_book` — active roster (~3,500 rows, main working set)
- `raw_check_ins` — check-in events
- `raw_payments` — payment events
- `raw_gps_48_hours` — GPS monitoring events

Normalized tables (written by ETL/migrations, not the import timer):
- `defendants` — merged on `idn`. Only `source IN ('blue_book','both')` is the active roster (~3,300).
- `cases` — one row per (idn, case_number)
- `payments`, `check_ins`, `gps_events` — cleaned mirrors of raw_* tables

Extension tables (written by the app itself):
- `notes`, `tags`, `court_dates`, `audit_log`, `violations`,
  `saved_searches`, `pinned_defendants`, `user_prefs`, `reminders`

Schema: `db/migrations/001_app_extensions_sqlite.sql`

---

## Allowed users

22 `@knoxsheriff.org` emails in `webapp/users.py` (Python) — will move to
a config file or the database in the Go rewrite.

---

## Current Python app (reference only — being replaced)

`webapp/` — FastAPI + Jinja2. Routes in `app.py`. Queries in `queries.py`
(T-SQL translated at runtime by `sqlite_compat.py` via sqlglot). Extension
queries in `queries_ext.py`. TTL cache (60s), clear with `GET /api/refresh`.

Keep this running on ptr1 until the Go rewrite is ready to swap in.

---

## Quirks in the source data (carry forward to Go rewrite)

- **Date formats:** ISO-with-Z, US with time, ISO without tz, junk. Need
  a flexible parser. All date columns are TEXT, not DATETIME.
- **Officer names are emails:** `Nicholas.Loveless@knoxsheriff.org` ->
  display as `Nicholas Loveless`. Strip domain, replace `.` with space.
- **Multi-case defendants:** case numbers stored as `@1606962, @1641152` comma-joined.
  `cases` table normalizes them to one row each.
- **Reserved word `order`:** `raw_gps_48_hours` has a column named `order`.
  In normalized `gps_events` it is renamed to `court_order`.

---

## Deployment commands (ptr1)

```bash
# Check everything is running
systemctl status ptr-webapp cloudflared ptr-import.timer

# App logs
journalctl -u ptr-webapp -f

# Tunnel logs
journalctl -u cloudflared -f

# Import logs
journalctl -u ptr-import.service -n 40 --no-pager

# Health check
curl -s http://127.0.0.1:8000/health

# Clear app cache
curl -s http://127.0.0.1:8000/api/refresh
```

---

## Conventions (carry forward to Go rewrite)

- All DB queries in one file (queries.go or similar) — not inline in handlers
- Date parsing in one helper function
- Officer display name in one helper function
- Secrets via env vars / `.env` file — never in the repo
- `/health` endpoint always auth-free (uptime monitoring)
- Never commit `.env`, `*.db`, CSV files with PII

## Don'ts

- Do not add any Azure dependencies
- Do not use the T-SQL shim in new code — write native SQLite
- Do not write directly to `raw_*` tables from the app
- Do not remove `/health` from the auth bypass
