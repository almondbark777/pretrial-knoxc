# PTR Web App — Claude Code Handoff Prompt

Paste this entire prompt at the start of a new Claude Code session.

---

## Project: Knox County Pre-Trial Services Web App

This is a web app for Knox County Sheriff's Office Pre-Trial division. Officers
look up defendants, check-in history, payment history, and GPS monitoring status.

**Repo:** `pretrial-knoxc` (already cloned locally — you are working in it now)

**Full project context is in CLAUDE.md at the repo root. Read it first.**

---

## Current situation

The app is currently written in Python (FastAPI + Jinja2 + SQLite). It is
pre-production — not yet in active use. Before we put it in front of real users,
we are doing the following:

1. **Server review of ptr1** (Linux server hosting the app) — we will paste
   diagnostic output for you to analyze.
2. **Rewrite the app in Go** — replacing the entire Python codebase with Go.
   Same routes, same templates (or cleaner versions), same auth flow, native
   SQLite queries (no T-SQL shim). Single binary deploy.
3. **Rename the database** from `kh222.db` to `pretrial_release.db`.
4. **Optimize for speed and readability** — the site must be fast and the code
   must be easy for a non-expert to read and maintain.

---

## Why Go

- Single binary: `go build` -> `scp` to ptr1 -> `systemctl restart ptr-webapp`. Done.
- No Python version issues, no venv, no pip on the server.
- Genuinely fast for this workload (22 users, SQLite, ~3,500 active records).
- Readable and boring — easy for someone new to follow.

---

## What stays the same on ptr1

- systemd service (`ptr-webapp`) — just swap the binary
- Cloudflare Tunnel (`cloudflared`) — untouched
- Cloudflare Zero Trust Access policy — untouched
- SQLite database — just renamed to `pretrial_release.db`
- `ptr-import.timer` (SharePoint CSV sync) — untouched

---

## Step 1: Server review

We will run `tools/ptr1_diag.sh` on ptr1 and paste the output here for you to
review. Tell us:
- Is the app healthy?
- Any disk/memory concerns?
- Anything that needs fixing before the rewrite?

---

## Step 2: Go rewrite plan

Once the server review is clean, plan and implement the Go rewrite:

### Recommended packages
- `modernc.org/sqlite` (pure Go SQLite driver, no CGO required — simpler build)
- `github.com/go-chi/chi/v5` for routing (lightweight, idiomatic)
- Standard library `html/template` for templates
- Standard library `net/http` for the server
- `github.com/gorilla/sessions` for session cookies

### Structure to aim for
```
pretrial-knoxc/
├── cmd/server/main.go      entry point
├── internal/
│   ├── db/                 all database queries (one function per query)
│   ├── handlers/           HTTP handlers (thin — call db, render template)
│   ├── auth/               session + Basic auth middleware
│   └── models/             plain Go structs for data shapes
├── templates/              html/template files
├── static/                 CSS, JS, images
├── db/                     SQLite file + migrations
└── deploy/                 systemd units, cloudflared config, setup.sh
```

### Auth (carry forward exactly)
Two gates:
1. Cloudflare Access (handled upstream — app sees `Cf-Access-Authenticated-User-Email` header)
2. App login: session cookie (12h) + HTTP Basic fallback. Single shared APP_PASSWORD.
   Allowed users: 22 @knoxsheriff.org emails (move to config or DB).

### Key routes to implement (same as current Python app)
- GET /                    dashboard
- GET /pretrial_app.html   case management
- GET /analytics.html      analytics
- GET /client_profile.html client profiles
- GET /login               login page
- POST /api/login          login endpoint
- POST /api/logout         logout
- GET /api/stats           dashboard stats JSON
- GET /api/defendants      case management bundle JSON
- GET /api/lookup?q=       live defendant search
- GET /health              health check (auth-free)
- GET /api/refresh         clear cache

(Full route list in webapp/app.py)

### Database quirks to handle in Go
- All date columns are TEXT — write a flexible date parser
- Officer names are emails (Nicholas.Loveless@knoxsheriff.org) — strip domain, replace . with space
- Multi-case defendants: case numbers stored comma-joined as "@1606962, @1641152"
- Table `raw_gps_48_hours` has a column named `order` — use quotes: `"order"`

---

## What good looks like when done

- `go build ./cmd/server` produces a single binary
- Binary runs on ptr1 (Linux amd64) with no other dependencies
- All pages load, data matches what the Python app showed
- `/health` returns `{"ok":true,"db":"up"}`
- Code is clean enough that someone new can read a handler and understand it in 2 minutes
