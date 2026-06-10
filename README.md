# pretrial-knoxc

Knox County Sheriff's Office — Pre-Trial Services web app. Officers track
defendants, check-ins, PTR fees, and GPS monitoring; supervisors correct and
delete bad data. Replaces a SharePoint + Excel workflow.

> **Current architecture (2026):** a single **Go** binary + **SQLite**, served
> behind a **Cloudflare Tunnel + Access**, self-hosted on `ptr1` (Linux) inside
> the office. This replaced the earlier Python/FastAPI + Azure SQL prototype (kept
> below as history); the business math now runs **server-side** as the single
> source of truth. The existing **client tracker stays the landing page** (`/`)
> during the transition — the **Case Console** app is one button away at
> `/console` (the classic `/dashboard` interface was removed 2026-06-09; its
> old URLs redirect to console equivalents).

## The Go app

**Build / run / test**
```bash
go build ./cmd/server          # -> ./server  (single binary, pure-Go SQLite, no CGO)
bash deploy/build-bundle.sh    # for ptr1: cross-compile (version-stamped) + tarball
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

**Routes** — `/` (client-tracker shell) and the **Case Console**:
`/console` (dashboard), `/console/clients` (+ `/new` intake wizard, `/{idn}`
record), `/console/{calendar,compliance,reports,admin}`. Printable reports at
`/reports` (Behind-on-GPS, Missed, EM-fees show-cause letters); CSV exports at
`/export/{behind,missed,cases,violations}.csv`; JSON at
`/api/{lookup,clients,stats,defendants,lookup_data,refresh}`; the CSRF-guarded
audited write side `/admin/{delete,restore,deleted,override,audit,…CRUD}`;
`/login`, `/metrics` (localhost-only), and `/health` (always auth-free).
Classic URLs (`/dashboard`, `/client_profile.html`, `/calendar.html`,
`/my_day.html`, …) 302 to their console equivalents.

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
`PHASE_8` two-server HA plan); `STATUS.md` is the single-glance state of the
project; `CONSOLE_DASHBOARD.md` documents the console; `deploy/DEPLOY_GO.md` +
`deploy/smoke.sh` cover the cutover and post-deploy verification.

**Repo layout (current)**

```
pretrial-knoxc/
├── cmd/server/            main: routes, middleware, graceful shutdown
├── internal/              compute / db / handlers / auth / models / emfees / metrics / build
├── templates/             server-rendered pages (console_*.html, reports, admin)
├── static/                console.css + the bundled client tracker (lookup/)
├── db/migrations/         001–006 extension-table migrations (also self-provisioned at startup)
├── deploy/                build-bundle.sh, install-on-ptr1.sh, monitoring + backup units, docs
├── tools/parity_ref.py    golden-value generator (Python port of the canonical JS)
└── webapp/                legacy Python app — reference only, superseded
```

**Deploying to ptr1**

```bash
bash deploy/build-bundle.sh          # dev box: cross-compile + stage → deploy/dist/ptr1-deploy.tar.gz
scp deploy/dist/ptr1-deploy.tar.gz alex@ptr1:~
ssh alex@ptr1 'tar xzf ptr1-deploy.tar.gz && cd ptr1-deploy && ./install-on-ptr1.sh'
curl -s https://ptr.<domain>/health  # check the stamped version took
```

The installer backs up the binary, unit, and DB before swapping, restarts
`ptr-webapp`, and health-checks. Rollback = restore the saved unit + restart.
Daily DB backups (`ptr-backup.timer` → `/mnt/backup/ptr`, 30-day retention,
integrity-checked) and Netdata monitoring with ntfy phone alerts are already
installed on the box — see `deploy/MONITORING.md` and `PHASE_5_BACKUP.md`.

**Security posture (current)**

- Two gates: Cloudflare Access (email allow-list + one-time code) in front of
  an app login (allow-listed email + shared `APP_PASSWORD`, 12 h session).
- Supervisor tier (`SUPERVISOR_EMAILS`) gates deletes, restores, overrides,
  and fee waivers; every write is audited in ET (`/admin/audit`).
- CSRF tokens on all `/admin/*` POSTs; security headers (nosniff,
  `X-Frame-Options: SAMEORIGIN`, Referrer-Policy); `COOKIE_SECURE` in prod.
- `/metrics` is localhost-only (Netdata scrapes it on-box); `/health` is the
  only auth-free public route. No PII leaves the server.
- Remaining hardening (tracked in `STATUS.md`): DB-backed allow-list instead
  of env, real SSO if funding allows, two-server HA at production scale-up.

**Data quirks to remember** (full list in `CLAUDE.md`) — mixed date formats in
TEXT columns (one flexible parser); officer names are emails (one display
helper); multi-case defendants comma-joined in the raw data; `order` is a
reserved-word column in `raw_gps_48_hours`.

---

## Historical — pre-overhaul Python / Azure prototype (reference only)

> The sections below describe the original FastAPI + Azure SQL build. The live
> system is the Go app above; this is retained for history.

A FastAPI site backed by an Azure SQL database, gated behind a shared-password
login for 22 allow-listed users.

- **Database** — 32K historical defendants + 3.3K active-roster loaded to
  Azure SQL; later mirrored to the SQLite file the Go app uses today.
- **App** — FastAPI + Jinja2, 10 pages, HTTP Basic Auth, 22-user allow-list.
- **Hosting** — ran locally only; the planned Azure App Service deployment was
  dropped in favor of self-hosting on `ptr1` (no cloud services except the
  Cloudflare tunnel). All Azure-era credentials and firewall rules were retired
  with the prototype.

The legacy code is still under `webapp/` for reference. It is not deployed,
not maintained, and its Azure-specific quirks (pymssql NVARCHAR bulk-copy,
serverless cold-starts, T-SQL `TRY_CONVERT`) no longer apply to the live system.

## License

Internal use — Knox County Sheriff's Office.
