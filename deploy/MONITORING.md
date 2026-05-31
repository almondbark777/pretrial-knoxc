# Monitoring ptr1

How we watch the box and the app, and how to act on what we see. Three layers,
each independent — set up as many as you want, in any order.

```
┌─ Layer 1: Netdata ──────────────┐   host: CPU/RAM/disk/network/IO/OOM
│  runs ON ptr1, localhost-only   │   service: ptr-webapp / cloudflared / import
│  scrapes the app /metrics       │   app: request rate / latency / errors
└─────────────────────────────────┘
┌─ Layer 2: app /metrics ─────────┐   built into the Go binary, Prometheus format
│  127.0.0.1:8000/metrics         │   feeds Netdata today; Grafana Cloud later
└─────────────────────────────────┘
┌─ Layer 3: external /health ping ┐   pings from OUTSIDE the office
│  UptimeRobot / Better Stack     │   catches "whole box / tunnel is down"
└─────────────────────────────────┘
```

Why this shape: Netdata answers *"how is the box doing?"* with near-zero setup;
the app `/metrics` endpoint answers *"what should I improve?"*; the external ping
is the only thing that can tell you the server is unreachable, because internal
monitoring goes dark exactly when you most need it.

---

## Layer 1 — Netdata (host + service + app dashboard)

Install with the script (run **on ptr1** as `alex`):

```bash
cd ~/ptr1-deploy          # wherever the deploy bundle is extracted
./install-netdata.sh
```

It installs Netdata (telemetry off), **binds the dashboard to `127.0.0.1` only**,
points the Prometheus collector at the app's `/metrics`, and watches the
`ptr-webapp`, `cloudflared`, and `ptr-import`/`ptr-backup` units. Netdata's
built-in system alarms (CPU, RAM, disk space + inodes, disk IO, network errors,
OOM) are left on — they're good defaults and need no tuning.

### Viewing the dashboard from outside the office (Cloudflare Access)

This is the chosen remote-view path: expose the dashboard as
`netdata-ptr.<domain>` behind the **same Cloudflare Access gate** the app already
uses. Nothing new leaves the box to a third party — Netdata stays bound to
`127.0.0.1`, cloudflared reaches it locally, and Access (your @knoxsheriff.org
email allowlist + one-time code) is the only way in. You then browse it from any
device, anywhere, with no SSH.

> ⚠️ **Order of operations matters.** Create the Access application **before** (or
> in the same change as) the tunnel ingress + DNS. If the hostname goes live
> without an Access policy attached, the dashboard is briefly **world-readable**.

**1. Add the ingress rule** on ptr1 — `/etc/cloudflared/config.yml` (see
[cloudflared-config.example.yml](cloudflared-config.example.yml)); the
`netdata-ptr` block goes *before* the `http_status:404` catch-all:

```yaml
ingress:
  - hostname: ptr.example.com
    service: http://127.0.0.1:8000
  - hostname: netdata-ptr.example.com      # <-- add this block
    service: http://127.0.0.1:19999
  - service: http_status:404               # must stay last
```

**2. Create the DNS route** for the new hostname (creates the proxied CNAME):

```bash
cloudflared tunnel route dns <TUNNEL-NAME-or-ID> netdata-ptr.example.com
```

**3. Create the Access application.** Cloudflare Zero Trust dashboard →
**Access → Applications → Add an application → Self-hosted**:
- Application domain: `netdata-ptr.example.com`
- Attach the **same policy** as the app (the @knoxsheriff.org email allowlist).
- Session duration: match the app (e.g. 24h).

**4. Apply** on ptr1:

```bash
sudo systemctl restart cloudflared
```

**5. Verify:** from a browser *outside* the office, go to
`https://netdata-ptr.example.com` → you should hit the Access login (email + code)
→ then the live Netdata dashboard. Confirm the **Prometheus → ptr_webapp** charts
and the **systemd** service states are populating.

**Do not** bind Netdata to `0.0.0.0` or open a firewall port — Access in front of a
localhost-bound Netdata is the whole point. The same pattern (extra hostname + same
Access policy) is how you'd remotely reach any other localhost-only tool later.

### Deep-dive fallback: SSH tunnel

For one-off debugging without touching Cloudflare, you can still tunnel in:

```bash
ssh -L 19999:127.0.0.1:19999 alex@ptr1
# then open http://localhost:19999
```

### Alert notifications (phone push — the "tell me without me looking" piece)

A dashboard only helps when you look at it; a push notification is what reaches
you when you're away. By default Netdata raises alarms but only shows them in the
dashboard — turn on a notifier to get pushed.

**`install-netdata.sh` offers this for you (step 7):** it prompts for an **ntfy
topic** and, if you give one, wires up phone-push alerts and sends a test. ntfy is
the lowest-friction option — free, instant phone push, no mail server, no account.
Just install the **ntfy app**, and subscribe to the same topic name.

> **Posture note:** the free public server `ntfy.sh` is a third party, and the
> alarm text passes through it (low-sensitivity — e.g. "ptr-webapp down", "disk at
> 90%", never defendant data). If even that crosses a line for you, two
> in-perimeter alternatives: **self-host ntfy** (a tiny container on ptr1 or
> another office box; point `DEFAULT_RECIPIENT_NTFY` at it), or use **email** to
> your @knoxsheriff.org address (needs a local MTA like `msmtp`). Pick a long,
> hard-to-guess topic name regardless — anyone who knows a public ntfy topic can
> read its alerts.

To set it up by hand or change it later, edit the notify config on ptr1
(`/etc/netdata` for a package install, `/opt/netdata/etc/netdata` for a static one):

```bash
cd /etc/netdata          # or /opt/netdata/etc/netdata
sudo ./edit-config health_alarm_notify.conf
```

Then set one of:
- **ntfy** — `SEND_NTFY="YES"`, `DEFAULT_RECIPIENT_NTFY="https://ntfy.sh/<your-topic>"`
- **Email** — `SEND_EMAIL="YES"`, `DEFAULT_RECIPIENT_EMAIL="you@knoxsheriff.org"` (needs an MTA)
- **Slack / Discord** — set the matching `SEND_*="YES"` + webhook URL

Apply with `sudo systemctl restart netdata`. Send a test (path also depends on
install type): `sudo /usr/libexec/netdata/plugins.d/alarm-notify.sh test` (or under
`/opt/netdata/usr/libexec/...`).

### Adding a custom app alarm

Once the dashboard shows the `ptr_webapp` charts, you can alarm on them. Find the
chart id under the **Prometheus → ptr_webapp** section, then:

```bash
cd /etc/netdata
sudo ./edit-config health.d/ptr-webapp.conf   # create it
```

Example — warn when the app starts returning 5xx (uses the scraped counter; adjust
the `on:` chart id to match what the dashboard shows):

```
template: ptr_webapp_5xx
      on: prometheus_ptr_webapp.ptr_http_requests_total
  lookup: sum -1m of *5xx*
   units: errors/min
   every: 30s
    warn: $this > 0
    crit: $this > 10
    info: ptr-webapp is returning server errors
      to: sysadmin
```

`sudo systemctl reload netdata` to apply.

---

## Layer 2 — App `/metrics` (already in the Go binary)

The server exposes Prometheus-format metrics at `http://127.0.0.1:8000/metrics`.
It is auth-free (like `/health`) but **localhost-only** — it is not in the
cloudflared ingress, so it never leaves the box. It exposes route names and
counts, never PII.

Series:

| Metric | Type | What it tells you |
|---|---|---|
| `ptr_http_requests_total{route,method,status}` | counter | Traffic + error rate per route (`status` = `2xx_3xx` / `4xx` / `5xx`) |
| `ptr_http_request_duration_seconds` | histogram | Latency per route — find the slow endpoints |
| `ptr_http_requests_in_flight` | gauge | Concurrent requests right now |
| `ptr_process_uptime_seconds` | gauge | Resets to ~0 on restart/crash — a crash-loop detector |
| `ptr_go_goroutines` | gauge | Leak signal if it climbs without bound |
| `ptr_go_memory_alloc_bytes` / `_sys_bytes` | gauge | Live heap / total memory from the OS |
| `ptr_go_gc_total` | counter | GC cycles — rising fast = allocation pressure |

Quick look without Netdata:

```bash
curl -s http://127.0.0.1:8000/metrics | grep ptr_http_request_duration_seconds_sum
```

> Note: requests rejected by auth (no session) bucket under `route="other"` —
> they never reached a route. Real authenticated officer traffic shows proper
> per-route patterns.

**Graduating to history (Grafana Cloud, optional, later):** the free tier scrapes
this same endpoint — install Grafana Alloy / a Prometheus agent on ptr1 pointed at
`127.0.0.1:8000/metrics` and `remote_write` to Grafana Cloud. No code changes; you
gain long-term trends and dashboards without running Prometheus/Grafana on the box.

---

## Layer 3 — External `/health` uptime ping

`/health` checks the DB and returns `{"ok":true,"db":"up"}`. An outside monitor
hitting it is the only thing that catches the whole box or the tunnel being down.

1. Pick a free monitor: **UptimeRobot** or **Better Stack** (free tiers cover this).
2. Monitor type **HTTPS**, URL `https://ptr.example.com/health`, interval 1–5 min.
3. Expect HTTP 200 and (Better Stack) keyword `"ok":true`.
4. Alerts: email / SMS / push to whoever's on call.

**The Access gotcha:** `https://ptr.example.com/health` sits *behind* Cloudflare
Access, so an external monitor gets bounced to the login screen, not the app. Pick
one:

- **(Recommended) Cloudflare Health Checks** — Cloudflare → Traffic → Health Checks.
  These originate *inside* Cloudflare, ahead of Access, so they reach `/health`
  cleanly. No credentials to manage.
- **Access Service Token** — create a service token (Zero Trust → Access → Service
  Auth), add a policy that allows it on the app, and set the monitor to send the
  `CF-Access-Client-Id` / `CF-Access-Client-Secret` headers. Works with UptimeRobot
  Pro / Better Stack custom headers.
- **Bypass path** — add an Access policy that bypasses `/health` only. Lowest
  friction, but it makes `/health` publicly reachable (it exposes nothing sensitive,
  so this is acceptable if you prefer it).

---

## At a glance — where to look when…

| Question | Look at |
|---|---|
| Is the site up right now (from outside)? | Layer 3 monitor / status page |
| Is the box healthy (CPU/RAM/disk)? | Netdata dashboard, system section |
| Did `ptr-webapp` crash / restart? | Netdata systemd section, or `ptr_process_uptime_seconds`, or `journalctl -u ptr-webapp` |
| Which endpoint is slow? | `ptr_http_request_duration_seconds` per route |
| Are users hitting errors? | `ptr_http_requests_total{status="5xx"}` |
| Is the disk filling with DB/backups? | Netdata disk-space alarm |
| Memory/goroutine leak? | `ptr_go_memory_alloc_bytes`, `ptr_go_goroutines` trending up |
