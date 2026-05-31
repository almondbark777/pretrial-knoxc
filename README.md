# pretrial-knoxc

Knox County Sheriff's Office — Pre-Trial Services web app. Officers track
defendants, check-ins, PTR fees, and GPS monitoring; supervisors correct and
delete bad data. Replaces a SharePoint + Excel workflow.

> **Current architecture (2026):** a single **Go** binary + **SQLite**, served
> behind a **Cloudflare Tunnel + Access**, self-hosted on `ptr1` (Linux) inside
> the office. This replaced the earlier Python/FastAPI + Azure SQL prototype (kept
> below as history); the business math now runs **server-side** as the single
> source of truth. The existing **client tracker stays the landing page** (`/`)
> during the transition — the new admin/data-entry app is one button away at
> `/dashboard`.

## The Go app

**Build / run / test**
```bash
go build ./cmd/server          # -> ./server  (single binary, pure-Go SQLite, no CGO)
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o server ./cmd/server   # for ptr1
go test ./...                  # compute + db + handlers + auth
APP_BASE_DIR=. SQLITE_DB_PATH=db/kh222.db APP_PASSWORD=dev go run ./cmd/server
```

**Layout** (`internal/`)
- `compute/` — business math, a faithful port of the canonical JS (check-in
  windows, PTR fees, GPS billing). The single source of truth; heavily tested.
- `db/` — native SQLite: `BuildClients` joins the `raw_*` tables and applies
  tombstones + field overrides; admin writes; the tracker's `/api/lookup_data` feed.
- `handlers/` — thin HTTP handlers + roster/stats/calendar/My-Day services.
- `auth/` — Cloudflare-Access header + 12h session cookie + Basic fallback;
  allow-list + supervisor tier; CSRF tokens on writes.
- `models/` — plain data shapes. `templates/` + `static/` — server-rendered UI
  (no SPA, framework, or CDN).

**Routes** — `/` (client-tracker shell), `/dashboard`, `/my_day.html`,
`/pretrial_app.html` (cases), `/analytics.html`, `/calendar.html` (per-client +
roster mode), `/client_profile.html`, `/export/{behind,missed,cases}.csv`,
`/api/{lookup,clients,stats,defendants,lookup_data,refresh}`, the CSRF-guarded
audited write side `/admin/{delete,restore,deleted,override,audit,…CRUD}`,
`/login`, and `/health` (always auth-free).

**Config** (env, or `webapp/.env`) — `APP_PASSWORD`, `APP_SESSION_SECRET`,
`SQLITE_DB_PATH`, `ALLOWED_EMAILS`, `SUPERVISOR_EMAILS`, `IMPORTER_RETIRED`,
`COOKIE_SECURE`. Template: `webapp/.env.example`.

**Key decisions** — server-side compute is authoritative; **delete is
importer-proof** (a `deleted_idns` tombstone filtered in `BuildClients`, so a
deleted person stays gone across the Sunday full reload — flips to a physical row
delete via `IMPORTER_RETIRED` at SharePoint cutover); delete / restore / override
are supervisor-gated; every write is audited (viewable at `/admin/audit`).

**Docs** — the spec is `PTR_MASTER_OVERHAUL_BRIEF.md` (parent folder); the
`PHASE_*.md` files are the append-only paper trail (`PHASE_7` admin/data-entry,
`PHASE_8` two-server HA plan); `deploy/DEPLOY_GO.md` + `deploy/smoke.sh` cover the
cutover and post-deploy verification.

---

## Historical — pre-overhaul Python / Azure prototype (reference only)

> The sections below describe the original FastAPI + Azure SQL build. The live
> system is the Go app above; this is retained for history.

A FastAPI site backed by an Azure SQL database, gated behind a shared-password
login for 22 allow-listed users.

- ✅ **Database loaded** — 32K historical defendants + 3.3K active-roster with
  full case/check-in/payment/GPS history on Azure SQL Database.
- ✅ **App built** — 10 pages wired to live data (dashboard, case management,
  client profile lookup, analytics, violations, 5 mockup tools).
- ✅ **Auth** — HTTP Basic Auth middleware, 22-user allow-list, shared password.
- ⏳ **Hosting** — ran locally only.
- ⏳ **Real SSO** — Microsoft Entra; superseded by the Cloudflare Access gate.

## Repo layout

```
pretrial-knoxc/
├── CLAUDE.md                        project memory for Claude Code
├── README.md                        (you are here)
├── .gitignore
├── .github/workflows/
│   └── azure-deploy.yml.example     rename to .yml when ready to auto-deploy
├── db/                              SQL schema + ETL scripts + built SQLite copy
│   ├── schema_azure_sql.sql
│   ├── load_azure.sql
│   ├── build_db.py, export_azure.py
│   ├── csv_clean/                   10 UTF-8 CSVs ready for BULK INSERT
│   ├── kh222.db                     SQLite mirror for offline reference
│   └── README.md                    schema + row counts + quirks
└── webapp/
    ├── app.py                       FastAPI routes + Basic Auth middleware
    ├── queries.py                   all SQL queries + pymssql pool + TTL cache
    ├── users.py                     22-user allow-list
    ├── requirements.txt
    ├── .env.example                 copy to .env, fill in secrets
    ├── templates/                   10 HTML pages (5 Jinja-patched)
    └── README.md                    app internals + API reference
```

## Running locally

Requires Python 3.10+ and git on your PATH.

```bash
cd webapp
python -m venv .venv
# Windows:      .venv\Scripts\Activate.ps1
# macOS/Linux:  source .venv/bin/activate
pip install -r requirements.txt
cp .env.example .env        # Windows: copy .env.example .env
# edit .env, paste your DB_PASSWORD, optionally change APP_PASSWORD
uvicorn app:app --reload --port 8000
```

Open http://localhost:8000 — browser prompts for:

- **Username:** any email in `webapp/users.py` (case-insensitive)
- **Password:** whatever `APP_PASSWORD` is set to (default `pretrialtestsite`)

First request after idle will take ~15 s — the serverless DB cold-starts.

## Making it live (without needing your laptop on)

Push to GitHub, deploy to **Azure App Service Free F1** ($0/mo), wire up
**GitHub Actions** for push-to-deploy. End state:

- Public URL: `<app-name>.azurewebsites.net`
- Users log in with their email + shared password from anywhere
- `git push` to main → ~2 min later the site is updated
- Works when your laptop is off, or you're on vacation

Open Claude Code in this folder and it will walk you through it — see
`CLAUDE.md` for the full project memory + deployment plan.

## Known quirks

- **pymssql.bulk_copy is broken on NVARCHAR** — stage into VARCHAR first,
  then server-side `INSERT SELECT`. See `CLAUDE.md` → "Quirks."
- **Azure SQL cold-start** — 15–30 s on first hit after auto-pause.
- **Mixed date formats** — source data has ISO, US, and Excel dates jumbled
  together. All date columns are `NVARCHAR(50)`. Use `TRY_CONVERT`.
- **`order` is a reserved word** — bracketed `[order]` in the schema.

## Security TODOs (before going wider)

- [ ] Rotate the Azure SQL admin password.
- [ ] Delete the wide-open `claude-sandbox` firewall rule in SQL Server → Networking.
- [ ] Create an `app_reader` DB user with only `db_datareader` role; switch the app to use it.
- [ ] Turn on Azure SQL auditing.
- [ ] HTTPS-only (App Service → TLS settings → HTTPS only → on).
- [ ] Swap shared-password auth for Microsoft Entra SSO when funding allows.

## License

Internal use — Knox County Sheriff's Office.
