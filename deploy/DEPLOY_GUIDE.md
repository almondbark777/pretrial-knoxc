# PTR Web App — Self-Hosting Deploy Guide (Linux + Cloudflare Tunnel)

This stands up the Pre-Trial web app on your own Linux server and exposes it to
the office through a **Cloudflare Tunnel**, gated behind **Cloudflare Access**
(login required). No ports are opened on your firewall — the server makes an
outbound connection to Cloudflare, and Cloudflare brings traffic back to it.

```
Office staff ─▶ https://ptr.<yourdomain>  ─▶  Cloudflare Access (login gate)
            ─▶ Cloudflare edge ─▶ Tunnel ─▶ cloudflared (on your server)
            ─▶ http://127.0.0.1:8000 (uvicorn / FastAPI) ─▶ Azure SQL
```

The app keeps its **own** login on top of Access, so there are two gates. That's
intentional for now (defendant PII). You can collapse it to one later — see
"Optional: single sign-on" at the end.

---

## What you need before starting

- A Linux server (Debian/Ubuntu) that stays on — physical box, VM, or mini-PC in
  the office. 1 vCPU / 1 GB RAM is plenty.
- `sudo` on that server.
- Your Cloudflare account with the domain already added (you confirmed this).
- The Azure SQL password (goes in `.env`, never committed).
- The deployment zip (`ptr-knoxc-deploy.zip`).

---

## Part A — Get the app onto the server

From your workstation:

```bash
scp ptr-knoxc-deploy.zip you@server:~/
ssh you@server
unzip ptr-knoxc-deploy.zip -d ptr-knoxc-deploy
cd ptr-knoxc-deploy
```

## Part B — Run the installer

```bash
sudo bash deploy/setup.sh
```

This installs Python, **FreeTDS** (required for pymssql → Azure SQL), and
**cloudflared**; creates the `ptrapp` service user; copies the app to
`/opt/ptr-knoxc`; builds the virtualenv; and registers the `ptr-webapp` systemd
service. It does **not** start the app yet — it has no secrets.

## Part C — Fill in the secrets

```bash
sudo nano /opt/ptr-knoxc/webapp/.env
```

Set these:

| Key | What to put |
|---|---|
| `DB_SERVER` | `ptrknoxc.database.windows.net` (already set) |
| `DB_NAME` | `PTR_databaseknoxc` (already set) |
| `DB_USER` | `abentley777` (already set) |
| `DB_PASSWORD` | your Azure SQL password |
| `APP_PASSWORD` | the shared password office staff type at the app login |
| `APP_SESSION_SECRET` | a random string — generate with `openssl rand -hex 32` |

Add the `APP_SESSION_SECRET` line if it isn't there. Then re-lock the file:

```bash
sudo chmod 600 /opt/ptr-knoxc/webapp/.env
sudo chown ptrapp:ptrapp /opt/ptr-knoxc/webapp/.env
```

## Part D — Allow the server through the Azure SQL firewall  ⚠️ easy to forget

Azure SQL rejects connections from unknown IPs. Find the server's public IP and
add it as a firewall rule.

```bash
curl -s https://ifconfig.me ; echo
```

Add that IP in the Azure portal: **SQL server `ptrknoxc` → Networking →
Firewall rules → Add a rule** (or with the CLI):

```bash
az sql server firewall-rule create \
  --resource-group <your-rg> --server ptrknoxc \
  --name office-host --start-ip-address <IP> --end-ip-address <IP>
```

> If the office internet has a dynamic IP, you may need to update this when it
> changes, or allow your IP range.

## Part E — Start the app and smoke-test it locally

```bash
sudo systemctl start ptr-webapp
sudo systemctl status ptr-webapp --no-pager
curl -s http://127.0.0.1:8000/health
```

`/health` should return `{"ok": true, "db": "up"}`. If it says `db` is down,
re-check `DB_PASSWORD` and the Azure firewall rule (Part D). The first request
after the database has been idle can take 15–30s — Azure SQL serverless
cold-starts; the app retries automatically.

Logs if you need them: `journalctl -u ptr-webapp -f`

## Part F — Create the Cloudflare Tunnel

```bash
cloudflared tunnel login           # opens/prints a URL — authorize your domain
cloudflared tunnel create ptr-knoxc
cloudflared tunnel route dns ptr-knoxc ptr.<yourdomain>
```

`create` prints a **Tunnel ID (UUID)** and writes a credentials JSON under
`~/.cloudflared/`. Move it where the service expects it and write the config:

```bash
sudo mkdir -p /etc/cloudflared
sudo cp ~/.cloudflared/<TUNNEL-ID>.json /etc/cloudflared/
sudo cp deploy/cloudflared-config.example.yml /etc/cloudflared/config.yml
sudo nano /etc/cloudflared/config.yml      # fill in <TUNNEL-ID> and ptr.<yourdomain>
```

Install cloudflared as a service and start it:

```bash
sudo cloudflared service install
sudo systemctl restart cloudflared
sudo systemctl status cloudflared --no-pager
```

At this point `https://ptr.<yourdomain>` should reach the app's login page.

## Part G — Lock it down with Cloudflare Access (login gate)  ⚠️ do this before sharing the URL

Until you add Access, anyone who learns the hostname can hit the app login.
Put an identity gate in front:

1. Cloudflare dashboard → **Zero Trust → Access → Applications → Add an
   application → Self-hosted**.
2. **Application domain:** `ptr.<yourdomain>`.
3. **Policy:** Action **Allow**, and add a rule that matches your staff — either:
   - **Emails** — paste the office addresses (the same people in
     `webapp/users.py`), or
   - **Emails ending in** `@knoxsheriff.org` (whatever your office domain is).
4. Choose a login method (one-time PIN to email works with zero extra setup;
   Google/Microsoft SSO is nicer if available).
5. Save.

Now staff hit Access first (verify email), then the app login.

## Part H — End-to-end check

From a machine that is **not** logged into Cloudflare:

1. Visit `https://ptr.<yourdomain>` → you should get the Cloudflare Access login.
2. Authenticate as an allowed user → you reach the app login.
3. Log in with an allowed email + `APP_PASSWORD` → you're in.
4. The PTR Client Lookup loads automatically at `/` and pulls live data from SQL — no CSV upload.

---

## Lookup-only deployment (what this is)

This package runs `app_lookup:app`, which serves ONLY the bundled PTR Client
Lookup app at `/`, fed live from SQL via `/api/lookup_data`. Every other path
redirects to `/`, so the dashboard, analytics, intake, etc. are not exposed. To
run the full multi-page app instead, change the service `ExecStart` to
`uvicorn app:app ...`.

The lookup app still keeps a manual CSV-upload screen as a fallback: if the
server data fetch ever fails, officers can drop in SharePoint CSVs the old way.

### Known data gap (GPS switch / notes)

The current `raw_gps_48_hours` table does not carry the "Switched To",
"Switched GPS Date", or "Notes" columns. So switch-aware GPS billing, GPS-relief
freezing, and the yellow "GPS fees waived" banner won't show until those columns
are added to the table and ETL. Everything else (profiles, check-ins, payments,
GPS day/dollar math, rosters, calendars) works from live SQL.

## Keeping data current

Because this runs against **live Azure SQL**, the lookup is always current — no
rebuilds needed. (This is the difference from the old self-contained
"PTR Client Lookup" HTML file, which carried a frozen data snapshot.) Updating
the *app itself* = redeploy: drop a new zip, re-run nothing but
`sudo systemctl restart ptr-webapp` after copying changed files, or just re-run
the relevant parts of `setup.sh`.

## Updating / restarting

```bash
sudo systemctl restart ptr-webapp     # after app changes
sudo systemctl restart cloudflared    # after tunnel config changes
journalctl -u ptr-webapp -f           # app logs
journalctl -u cloudflared -f          # tunnel logs
```

## Security notes

- The app listens on **127.0.0.1 only** — it is never directly reachable on the
  LAN or internet; all access is through the tunnel.
- `.env` holds the DB password — keep it `chmod 600`, owned by `ptrapp`, and
  never commit it. (It's already gitignored.)
- This is defendant PII. Keep the Access policy tight and review who's on it.
- Rotate `APP_PASSWORD` / `DB_PASSWORD` periodically; after rotating
  `APP_PASSWORD`, set `APP_SESSION_SECRET` so existing sessions don't break
  unexpectedly.

## Optional: single sign-on (collapse the two logins)

Later, you can have the app trust Cloudflare Access instead of its own login:
Access forwards a signed `Cf-Access-Authenticated-User-Email` header, which the
app can read to identify the user, dropping the second password prompt. Ask me
when you want this — it's a small change to `app.py`'s auth middleware.

## Troubleshooting

| Symptom | Likely cause / fix |
|---|---|
| `/health` shows `db` down | Wrong `DB_PASSWORD`, or server IP not in Azure SQL firewall (Part D). |
| First load takes ~20s then works | Azure SQL serverless cold start — normal. |
| `pymssql` install/connect errors | FreeTDS missing — `sudo apt-get install -y freetds-bin freetds-dev`, then rebuild venv. |
| Hostname won't resolve / 1033 error | `cloudflared tunnel route dns` not run, or config hostname mismatch. |
| Anyone can reach the login page | Cloudflare Access app/policy not created yet (Part G). |
