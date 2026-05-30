# CLAUDE.md

Project memory for the `pretrial-knoxc` repo. Read this before touching code.

---

## What this project is

A web app for the Knox County Sheriff's Office Pre-Trial Services division.
Officers look up defendants, see their case info, check-in history, payment
history, and GPS monitoring status. Replaces a SharePoint-list-plus-Excel
workflow. Currently in active use on ptr1 (self-hosted Linux server).

Repo layout:

```
pretrial-knoxc/
├── db/         schema, migrations, SQLite database (kh222.db)
├── deploy/     setup scripts, systemd units, cloudflared config
├── tools/      helper/utility scripts
└── webapp/     FastAPI + Jinja2 app (main deliverable)
```

---

## Infrastructure — how it runs

**Everything is self-hosted. No Azure.**

- **Server:** ptr1 — a Linux box running at the office (Ubuntu/Debian).
- **App:** FastAPI + uvicorn, bound to `127.0.0.1:8000` only (never exposed directly).
- **Database:** SQLite at `/opt/ptr-knoxc/db/kh222.db`. Kept current by a systemd
  timer (`ptr-import.timer`) that polls a Gmail mailbox for CSV exports from
  Power Automate (see `deploy/SHAREPOINT_SYNC.md`).
- **Tunnel:** Cloudflare Tunnel (`cloudflared`) running as a systemd service.
  Config at `/etc/cloudflared/config.yml`. Routes `https://ptr.<domain>` → `http://127.0.0.1:8000`.
- **Outer auth gate:** Cloudflare Zero Trust Access policy — staff must have their
  email in the allowed list AND verify via emailed one-time code before reaching
  the app at all.
- **Inner auth gate:** App-level shared password (`APP_PASSWORD` in `.env`).
  Usernames are the 22 `@knoxsheriff.org` emails in `webapp/users.py`.
  Either session cookie (browser) or HTTP Basic (scripts/curl) — same password.

Access flow:
```
Staff browser -> https://ptr.<domain>
  -> Cloudflare Access (email in allowlist + one-time code emailed to them)
  -> App login page (their @knoxsheriff.org email + APP_PASSWORD)
  -> Inside the app
```

---

## The database

Local SQLite file: `/opt/ptr-knoxc/db/kh222.db`

Source data comes from SharePoint via Power Automate → Gmail → `sharepoint_import.py`.
The import timer refreshes four raw tables daily (~05:00):
- `raw_blue_book` — active roster (main working set, ~3,500 rows)
- `raw_check_ins` — check-in events
- `raw_payments` — payment events
- `raw_gps_48_hours` — GPS monitoring events

Normalized tables (written by migrations/ETL, not by the import timer):
- `defendants` — merged + deduped on `idn`. `source` = `blue_book` / `master_list` / `both`.
  Webapp only shows `source IN ('blue_book','both')` — the active roster (~3,300).
- `cases` — one row per (idn, case_number)
- `payments` / `check_ins` / `gps_events` — cleaned mirrors of raw_* tables

Extension tables (added by migration 001, written by the webapp itself):
- `notes`, `tags`, `court_dates`, `audit_log`, `violations`, `saved_searches`,
  `pinned_defendants`, `user_prefs`, `reminders`

Full schema: `db/migrations/001_app_extensions_sqlite.sql`

---

## The webapp

FastAPI + Jinja2. Templates are the original mockup HTML files with surgical
Jinja injection — the existing client-side JS (search, filter, pagination,
modal, charts) is preserved. Do not rewrite templates from scratch; only patch
data-binding points.

All queries live in `queries.py`. `queries_ext.py` handles the extension tables
(notes, tags, court dates, etc.). Every route pulls through a TTL cache (default
60s). Hit `/api/refresh` to clear it.

SQL dialect: queries are written in T-SQL (TOP, dbo., CONCAT, etc.) and
translated to SQLite at runtime by `sqlite_compat.py` via sqlglot. Do not
switch dialects — the shim handles it.

---

## Auth

Two layers (see Infrastructure above).

App-level auth is in `app.py` middleware:
- Session cookie for browsers (`kh_sess`, 12h, signed with `APP_SESSION_SECRET`)
- HTTP Basic as fallback for scripts/curl
- Public paths (no auth): `/health`, `/static/*`, `/login`, `/api/login`, `/api/logout`
- Allowed users: `webapp/users.py` — 22 `@knoxsheriff.org` emails

---

## Quirks and gotchas

### 1. SQL dialect shim
All SQL in `queries.py` is T-SQL. `sqlite_compat.py` translates it to SQLite
via sqlglot at execute time. It also strips `dbo.`, converts `%s` → `?`,
and registers custom UDFs (YEAR, MONTH, TRY_PARSE_DATE). If you add a new
query and it fails on SQLite, check the translation — complex T-SQL may need
a hint or manual rewrite.

### 2. Mixed date formats in source data
Source dates arrive as ISO-with-Z, US with time, ISO without tz, and junk.
`queries._fmt_date()` handles all cases. All date columns in the schema are
TEXT, not DATETIME. Use `TRY_PARSE_DATE(col)` for server-side date filtering.

### 3. Reserved word `order`
`raw_gps_48_hours.order` collides with SQL. Bracketed as `[order]` in schema.
In normalized `gps_events` it's renamed to `court_order`.

### 4. Officer names are emails
`defendants.supervising_officer` stores addresses like
`Nicholas.Loveless@knoxsheriff.org`. Use `queries._fmt_officer()` to convert
to display form (`Nicholas Loveless`). Do not re-roll the split.

### 5. Multi-case defendants
Some defendants have multiple case numbers stored as `@1606962, @1641152`.
Raw tables preserve the comma-joined string; `cases` table splits them into rows.
Webapp shows them joined with `, ` prefixed `@`.

---

## Deployment

Deploying a change to ptr1:

```bash
# From workstation — copy changed files
scp webapp/app.py alex@ptr1:/opt/ptr-knoxc/webapp/
# On ptr1
sudo systemctl restart ptr-webapp
```

Or re-run `setup.sh` for a full redeploy from a zip.

Logs on ptr1:
```bash
journalctl -u ptr-webapp -f        # app logs
journalctl -u cloudflared -f       # tunnel logs
journalctl -u ptr-import.service   # SharePoint import logs
```

---

## Conventions

- All SQL → `queries.py` or `queries_ext.py`. Nothing inline in routes.
- Date formatting → `queries._fmt_date()`
- Officer display → `queries._fmt_officer()`
- Money → `queries._d()` coerces Decimal|str|None → float
- Template patches → surgical only; mockups are the UI contract
- Secrets → env vars only, never in the repo
- Cache keys in `queries.cached(key, ttl, fn)` are string constants

## Don'ts

- Do not use `pymssql` — removed. SQLite only via `sqlite_compat.py`.
- Do not add Azure dependencies of any kind.
- Do not commit `.env`, `*.db`, or CSV files with PII.
- Do not write directly to `raw_*` tables from the app — they are overwritten
  by the import timer.
- Do not remove `/health` from the auth bypass — uptime monitors need it.
- Do not rerun `db/build_db.py` unless source CSVs changed — it is heavy and
  will overwrite any server-side edits to the DB.
