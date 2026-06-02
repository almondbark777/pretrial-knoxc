# Deploying the Go app to `ptr1`

The Go rewrite is a **single binary** + `templates/` + `static/`. No Python, venv,
or pip on the server. Cloudflare Tunnel, the Access policy, and the import timer are
**untouched** — you only swap the systemd service and ship the files.

> Status: this is the cutover checklist. Do it after Phase 5 backups + the DB rename
> (see `WORKFLOW.md`). The app keeps the existing client tracker as the landing page;
> the new admin/data-entry app lives behind the "Admin & Data-Entry →" button.

## 0. Prerequisites (once)
- Go 1.26+ on the build box (Windows dev box is fine — pure-Go SQLite cross-compiles).
- The 001 + 003 migrations are applied to the DB **or** rely on the app's startup
  `EnsureSchema` (it runs `CREATE TABLE IF NOT EXISTS` for the admin/extension tables).
- `/opt/ptr-knoxc/webapp/.env` holds config (see `webapp/.env.example`). Add the new
  keys: `APP_SESSION_SECRET` (set it!), optional `ALLOWED_EMAILS`, `SUPERVISOR_EMAILS`
  (so a supervisor can delete/override), `IMPORTER_RETIRED=false`.

## 1. Build (Linux amd64, single binary)
```bash
# Prefer deploy/build-bundle.sh (stamps the version + tars everything). Manual:
VERSION="$(git rev-parse --short HEAD)-$(date +%Y%m%d)"
GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags="-s -w -X pretrial-knoxc/internal/build.Version=$VERSION" -o server ./cmd/server
```
The version stamp is reported by `/health` (`"version"`) and the `ptr_build_info`
metric, so you can confirm the right binary is live after a deploy.

## 2. Ship binary + templates/ + static/  (static/lookup/ carries the tracker bundle)
```bash
scp ./server alex@ptr1:~
scp -r templates static alex@ptr1:~          # static/lookup/PTR_Client_Lookup.html included
```

## 3. Install on ptr1
```bash
sudo install -m 0755 ~/server /opt/ptr-knoxc/server
sudo rm -rf /opt/ptr-knoxc/templates /opt/ptr-knoxc/static
sudo cp -r ~/templates ~/static /opt/ptr-knoxc/
sudo chown -R ptrapp:ptrapp /opt/ptr-knoxc/{server,templates,static}

# First time only: install the Go service unit (replaces the Python ExecStart).
sudo cp /opt/ptr-knoxc/deploy/ptr-webapp-go.service /etc/systemd/system/ptr-webapp.service
sudo systemctl daemon-reload
```

## 4. Restart + smoke test
```bash
sudo systemctl restart ptr-webapp
curl -s http://127.0.0.1:8000/health            # {"db":"up","ok":true}

# Full automated smoke test (every page, exports, CSRF + auth gating). Basic auth,
# so use a SUPERVISOR email + APP_PASSWORD:
BASE=http://127.0.0.1:8000 EMAIL=you@knoxsheriff.org PW="$APP_PASSWORD" bash deploy/smoke.sh
# Through the tunnel, in a browser (Cloudflare Access will gate you):
#   /                     -> client tracker (landing) with "Admin & Data-Entry →"
#   /dashboard            -> new app; KPIs + rosters; "← Client Tracker" back
#   /api/lookup_data      -> 200 JSON (the tracker's data feed)
#   /admin/deleted        -> visible only to a SUPERVISOR_EMAILS user
```

## 5. Verify the headline features on real data
- Search a wrongly-added person → open their profile → **Delete person** (supervisor)
  → confirm → they vanish from the tracker, dashboard, cases, rosters, lookup; the
  audit row is in `audit_log`; **Deleted records** lets you restore.
- A non-supervisor sees **no** Delete/override controls and gets 403 if they POST.
- Add a note/tag/court-date as a regular officer → it persists on the profile.

## Rollback
Keep the previous `server` binary (e.g. `server.prev`). To revert:
```bash
sudo install -m 0755 /opt/ptr-knoxc/server.prev /opt/ptr-knoxc/server
sudo systemctl restart ptr-webapp
```
To fall back to the **Python** app entirely, restore the old `ExecStart`
(`app_lookup:app`) in the unit and `systemctl daemon-reload && restart`.

## Notes
- `/health` is auth-free (tunnel/uptime probes) — do not gate it.
- The app never writes to `raw_*` tables except the `IMPORTER_RETIRED=true` physical
  -delete path; the importer remains the owner of `raw_*`.
- DB path: the unit defaults to `pretrial_release.db`; until the rename, set
  `SQLITE_DB_PATH=/opt/ptr-knoxc/db/kh222.db` in `.env`.
