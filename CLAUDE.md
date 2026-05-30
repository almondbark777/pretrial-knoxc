# CLAUDE.md

Project memory for Claude Code working on `pretrial-knoxc`.  Read this
before touching code.  Update it when you learn something worth remembering.

## What this project is

A web app for the Knox County Sheriff's Office Pre-Trial Services division.
Officers look up defendants, see their case info, check-in history, payment
history, and GPS monitoring status.  Replaces a SharePoint-list-plus-Excel
workflow.  Currently a trial — run locally (or soon on Azure App Service),
demoed to a handful of users before asking the sheriff's office for real
production funding.

Repo layout:

```
pretrial-knoxc/
├── db/         ETL scripts + schema + built SQLite copy (offline reference)
└── webapp/     FastAPI + Jinja + pymssql app (main deliverable)
```

## The database

Hosted on Azure SQL Database, `ptrknoxc.database.windows.net / PTR_databaseknoxc`,
serverless General-Purpose tier (auto-pauses when idle).

Ten tables:

- `raw_master_list` (32,279) — 8-column SharePoint export, closed / historical
- `raw_blue_book` (3,509) — 40-column active roster with full details
- `raw_payments` (1,713) · `raw_check_ins` (2,046) · `raw_gps_48_hours` (698) — event feeds
- `defendants` (25,893) — merged + deduped on `idn` from master_list + blue_book.
  `source` column = `'both'` / `'blue_book'` / `'master_list'`.
  The webapp only shows source IN ('blue_book','both') — the active roster (3,309).
- `cases` (9,864) — normalized one-row-per-(idn, case_number)
- `payments` / `check_ins` / `gps_events` — cleaned mirrors of the `raw_*` tables, FK'd to `defendants.idn`
- View `v_defendant_summary` — per-defendant counts + sum of payments

Full schema in `db/schema_azure_sql.sql`.  Expected row counts are documented
in `db/README.md` and verified after every ETL run.

## Quirks and gotchas — do not re-learn these the hard way

### 1. pymssql.bulk_copy is broken on NVARCHAR columns

If you ever bulk-load data via `pymssql.Connection.bulk_copy()`, **NVARCHAR
columns will be corrupted**: the driver sends the string as raw ASCII bytes
and SQL Server stores them as UCS-2, producing byte-swapped mojibake
("blue_book" → "汢敵扟潯").

**Workaround:** stage into a VARCHAR temp table first, then
`INSERT INTO target SELECT * FROM stage` — the server-side VARCHAR → NVARCHAR
cast is done in-engine and produces correct output.  This is what
`fix_load.py` (referenced in `db/README.md`) does.

The webapp is read-only so this doesn't affect it, but any future write
paths need to avoid bulk_copy-on-NVARCHAR.

### 2. Azure SQL serverless auto-pause

First request after ~1 hour idle takes 15–30 seconds — the DB cold-starts.
Subsequent requests are fast.  `queries.py` retries on error 40613
("Database is not currently available").  If you want to avoid cold starts,
upgrade to provisioned General-Purpose (not worth the cost yet).

### 3. Mixed date formats in source data

Source dates arrive as ISO-with-Z (`2026-02-23T19:42:00Z`), US with time
(`11/25/2025 12:50`), ISO without tz, and occasional junk.  `queries.py`
has `_fmt_date()` that tries ISO first, falls back through `%m/%d/%Y %H:%M`,
`%m/%d/%Y`, `%Y-%m-%d %H:%M:%S`, `%Y-%m-%d`, and returns the raw string if
nothing matches.  Don't assume parseable.

All date columns in the schema are `NVARCHAR(50)`, not `DATETIME`.  If you
need to filter by date server-side, wrap in `TRY_CONVERT(datetime2, col)`.

### 4. Reserved word `order`

`raw_gps_48_hours.order` (court order) collides with SQL.  It's bracketed
as `[order]` in the schema.  In normalized `gps_events` it's renamed to
`court_order`.

### 5. Officer names are emails

`defendants.supervising_officer` and related columns store addresses like
`Nicholas.Loveless@knoxsheriff.org`.  `queries._fmt_officer()` converts
these to display form (`Nicholas Loveless`).  Use that helper; don't
re-roll the split.

### 6. Multi-case defendants

Some defendants have multiple case numbers stored as `@1606962, @1641152`.
`raw_*` tables preserve the comma-joined string; the normalized `cases`
table splits them into rows.  The webapp shows them joined with `, `
prefixed `@`.

## The webapp

FastAPI + Jinja.  Templates are the original mockup HTML files with
surgical Jinja injection — the existing client-side JS (search, filter,
pagination, modal, charts) is preserved.  Do not rewrite the templates
from scratch; only patch the data-binding points.

Injection patterns already in place:

- `index.html` — individual Jinja tokens for each stat, loops for officer
  caseloads and activity feed.
- `pretrial_app.html` — `const RAW = {{ raw_data|tojson|safe }};`
  replaces the mockup's hardcoded `RAW` object.
- `analytics.html` — `<script>window.__KH222 = {{ ... }};</script>` before
  the Chart.js block; each chart's `labels` and `data` arrays read from it.
- `violation.html` — `const defendants = {{ defendants|tojson|safe }};`
  replaces the hardcoded array.
- `client_profile.html` — `const SERVER_DATA = {{ data|tojson|safe }};`;
  the upload UI was removed wholesale since the DB is live.

All queries live in `queries.py`.  Every HTTP route pulls through a tiny
TTL cache keyed by a string constant (default 60 s).  Hit `/api/refresh`
to clear it.  Don't add ad-hoc queries in the routes — put them in
`queries.py`.

## Auth

`webapp/app.py` has an HTTP Basic Auth middleware.  It gates every route
except `/health`, `/static/*`, `/favicon.ico`.

- Usernames: fixed list of 22 Knox County emails in `webapp/users.py`.
  Matched case-insensitively.
- Password: single shared secret in `APP_PASSWORD` env var (default
  `pretrialtestsite` — override in production).
- `secrets.compare_digest()` for constant-time password comparison.
- Failure returns 401 with `WWW-Authenticate: Basic realm=...`.

**This is a demo-only auth layer.**  Before real production rollout, replace
with Microsoft Entra Easy Auth on Azure App Service (one portal toggle,
individual accounts, automatic @knoxsheriff.org restriction).  See
"Deployment plan" below.

## Running locally

```bash
cd webapp
python -m venv .venv && source .venv/bin/activate   # or .venv\Scripts\Activate.ps1
pip install -r requirements.txt
cp .env.example .env    # edit: fill DB_PASSWORD; optionally override APP_PASSWORD
uvicorn app:app --reload --port 8000
```

Don't commit `.env`.  It's in `.gitignore` already.

## Deployment plan (phased)

1. **Phase 1 — what we have now.** Run locally, Basic Auth, shared password.
   For show-and-tell demos only.
2. **Phase 2 — Azure App Service Free F1 / Basic B1.** Push to GitHub,
   connect App Service Deployment Center.  Keep Basic Auth.  URL is
   `<appname>.azurewebsites.net`.  Good for broader testing; people in the
   `users.py` list can access from anywhere with browser + shared password.
3. **Phase 3 — real prod.**  Switch to Basic B1 or Standard S1, enable
   Microsoft Entra Easy Auth, restrict to the Knox County tenant, put a
   Private Endpoint on the SQL server, rotate the admin password, create
   a `app_reader` DB role with SELECT-only, enable Azure SQL auditing,
   set up GitHub Actions for CI/CD, add a custom domain
   (`pretrial.knoxsheriff.org`).

Placeholder GitHub Actions workflow lives at
`.github/workflows/azure-deploy.yml.example` — rename to `.yml` and fill in
`publish-profile` secret when ready.

## Conventions

- All SQL → `queries.py`.  Nothing inline in routes.
- Date formatting → `queries._fmt_date`.
- Officer display → `queries._fmt_officer`.
- Money → `queries._d` coerces Decimal|str|None → float.
- Template patches → keep surgical; the mockups are the UI contract.
- Secrets → env vars, never in the repo.
- Cache keys in `queries.cached(key, ttl, fn)` are string constants — don't
  use mutable objects.

## What's done

- [x] ETL from 5 source files → SQLite + Azure SQL
- [x] FastAPI app serving 10 HTML pages against live SQL
- [x] Dashboard with live stats + officer caseloads + recent activity
- [x] Case Management (pretrial_app) with 3,309 defendants and their full event history
- [x] Client Profile lookup with GPS coverage math + day adjustment
- [x] Analytics with 6 live charts
- [x] Violation form with defendant search (read-only; no submit yet)
- [x] HTTP Basic Auth with 22-user allow-list

## What's next (rough priority)

1. **Deploy to Azure App Service** — Phase 2.  Set up from GitHub, hand the
   URL to the initial demo users.
2. **Violation submit endpoint** — `POST /api/violations` writes a new row.
   Needs a new `violations` table in SQL.
3. **Intake submit endpoint** — `POST /api/intake` creates a defendant + cases.
4. **Drug screen log** — the mockup has a "Log" button; add a `drug_screens`
   table and a write endpoint.
5. **Defendant location map** — the "coming soon" card on the dashboard.
   Needs lat/long columns somewhere (maybe `gps_events` extended).
6. **Phase 3 security** — Entra auth, private endpoint, read-only DB user,
   auditing, custom domain.
7. **PWA / mobile polish** — the mockups are desktop-first; review on phones.

## Don'ts

- Don't rerun `db/build_db.py` unless the source CSVs changed.  It's heavy
  and you'll lose any server-side edits.
- Don't commit `.env`, `*.db`, or CSV files with PII in them.
- Don't use `pymssql.bulk_copy` on NVARCHAR columns (see Quirk #1).
- Don't write directly to the `raw_*` tables from the app — they're
  source-of-truth; write to new normalized tables or add event tables.
- Don't remove the `/health` path from the Basic Auth bypass — App Service
  uptime probes need it.
