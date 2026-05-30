# PTR Web App — Self-Hosting Deploy Guide (Linux + Cloudflare Tunnel)

This stands up the Pre-Trial web app on your own Linux server and exposes it to
the office through a **Cloudflare Tunnel**, gated behind **Cloudflare Access**
(login required). No ports are opened on your firewall — the server makes an
outbound connection to Cloudflare, and Cloudflare brings traffic back to it.

```
Office staff -> https://ptr.<yourdomain>  ->  Cloudflare Access (login gate)
            -> Cloudflare edge -> Tunnel -> cloudflared (on your server)
            -> http://127.0.0.1:8000 (uvicorn / FastAPI) -> SQLite (db/kh222.db)
```

The app keeps its **own** login on top of Access, so there are two gates. That's
intentional for now (defendant PII). You can collapse it to one later -- see
"Optional: single sign-on" at the end.

---

## What you need before starting

- A Linux server (Debian/Ubuntu) that stays on -- physical box, VM, or mini-PC in
  the office. 1 vCPU / 1 GB RAM is plenty.
- `sudo` on that server.
- Your Cloudflare account with the domain already added.
- The deployment zip (`ptr-knoxc-deploy.zip`).

---

## Part A -- Get the app onto the server

From your workstation:

```bash
scp ptr-knoxc-deploy.zip you@server:~/
ssh you@server
unzip ptr-knoxc-deploy.zip -d ptr-knoxc-deploy
cd ptr-knoxc-deploy
```

## Part B -- Run the installer

```bash
sudo bash deploy/setup.sh
```

This installs Python and **cloudflared**; creates the `ptrapp` service user;
copies the app to `/opt/ptr-knoxc`; builds the virtualenv; and registers the
`ptr-webapp` systemd service. It does **not** start the app yet -- it has no
secrets.

## Part C -- Fill in the secrets

```bash
sudo nano /opt/ptr-knoxc/webapp/.env
```

Set these:

| Key | What to put |
|---|---|
| `APP_PASSWORD` | the shared password office staff type at the app login |
| `APP_SESSION_SECRET` | a random string -- generate with `openssl rand -hex 32` |

Then re-lock the file:

```bash
sudo chmod 600 /opt/ptr-knoxc/webapp/.env
sudo chown ptrapp:ptrapp /opt/ptr-knoxc/webapp/.env
```

## Part D -- Start the app and smoke-test it locally

```bash
sudo systemctl start ptr-webapp
sudo systemctl status ptr-webapp --no-pager
curl -s http://127.0.0.1:8000/health
```

`/health` should return `{"ok": true, "db": "up"}`. If it returns an error,
check that `db/kh222.db` exists and is readable by the `ptrapp` user. Logs:
`journalctl -u ptr-webapp -f`

## Part E -- Create the Cloudflare Tunnel

```bash
cloudflared tunnel login           # opens/prints a URL -- authorize your domain
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

At this point `https://ptr.<yourdomain>` should reach the app login page.

## Part F -- Lock it down with Cloudflare Access  WARNING: do this before sharing the URL

Until you add Access, anyone who learns the hostname can hit the app login.
Put an identity gate in front:

1. Cloudflare dashboard -> **Zero Trust -> Access -> Applications -> Add an
   application -> Self-hosted**.
2. **Application domain:** `ptr.<yourdomain>`.
3. **Policy:** Action **Allow**, and add a rule that matches your staff -- either:
   - **Emails** -- paste the office addresses (same people in `webapp/users.py`), or
   - **Emails ending in** `@knoxsheriff.org` (your office domain).
4. Choose a login method (one-time PIN to email works with zero extra setup;
   Google/Microsoft SSO is nicer if available).
5. Save.

Now staff hit Access first (verify email), then the app login.

## Part G -- End-to-end check

From a machine that is **not** logged into Cloudflare:

1. Visit `https://ptr.<yourdomain>` -- you should get the Cloudflare Access login.
2. Authenticate as an allowed user -> you reach the app login.
3. Log in with an allowed email + `APP_PASSWORD` -> you're in.

---

## Data / database

The app reads from a local SQLite file at `/opt/ptr-knoxc/db/kh222.db`.
This file is kept current by the SharePoint import timer (`ptr-import.timer`),
which polls a mailbox for CSV exports from Power Automate. See
`SHAREPOINT_SYNC.md` for setup.

To run the full multi-page app (dashboard, analytics, etc.) instead of the
lookup-only build, change the service `ExecStart` to `uvicorn app:app ...`.

## Updating / restarting

```bash
sudo systemctl restart ptr-webapp     # after app changes
sudo systemctl restart cloudflared    # after tunnel config changes
journalctl -u ptr-webapp -f           # app logs
journalctl -u cloudflared -f          # tunnel logs
```

## Security notes

- The app listens on **127.0.0.1 only** -- never directly reachable on the LAN
  or internet; all access goes through the tunnel.
- `.env` holds the app secrets -- keep it `chmod 600`, owned by `ptrapp`, never
  commit it. (Already gitignored.)
- This is defendant PII. Keep the Access policy tight and review who's on it.
- Rotate `APP_PASSWORD` periodically; set `APP_SESSION_SECRET` explicitly so
  existing sessions don't break unexpectedly when the password rotates.

## Optional: single sign-on (collapse the two logins)

Later, you can have the app trust Cloudflare Access instead of its own login:
Access forwards a signed `Cf-Access-Authenticated-User-Email` header, which the
app can read to identify the user, dropping the second password prompt. Ask when
you want this -- it's a small change to `app.py`'s auth middleware.

## Troubleshooting

| Symptom | Likely cause / fix |
|---|---|
| `/health` shows `db` down | `db/kh222.db` missing or not owned by `ptrapp`. Check path and permissions. |
| Hostname won't resolve / 1033 error | `cloudflared tunnel route dns` not run, or config hostname mismatch. |
| Anyone can reach the login page | Cloudflare Access app/policy not created yet (Part F). |
