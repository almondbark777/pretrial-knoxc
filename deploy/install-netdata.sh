#!/usr/bin/env bash
# install-netdata.sh — set up Netdata host + service + app monitoring on ptr1.
# Run ON ptr1 as `alex`. Uses sudo for privileged steps (asked once). Safe +
# re-runnable: skips the install if Netdata is already present, and rewrites the
# two collector configs each run (idempotent).
#
# What it does:
#   - installs Netdata (stable, telemetry off) if not already installed
#   - binds the dashboard to 127.0.0.1 only (never public; view via SSH tunnel
#     or behind Cloudflare Access — see deploy/MONITORING.md)
#   - scrapes the app's /metrics endpoint (request rate / latency / errors)
#   - watches the ptr-webapp, cloudflared, and ptr-import systemd units
#   - leaves Netdata's rich built-in system alarms (CPU/RAM/disk/IO/OOM) on
#
# After this runs, see deploy/MONITORING.md for viewing + alert-notification setup.
set -euo pipefail

NDDIR=/etc/netdata
APP_METRICS_URL="http://127.0.0.1:8000/metrics"

say(){ printf '\n\033[1m== %s ==\033[0m\n' "$*"; }

say "0. sudo"
sudo -v

say "1. install Netdata (skip if present)"
if command -v netdata >/dev/null 2>&1 || systemctl list-unit-files 2>/dev/null | grep -q '^netdata\.service'; then
  echo "  Netdata already installed — skipping install, just (re)configuring."
else
  echo "  running official kickstart (stable channel, telemetry disabled)…"
  wget -qO /tmp/netdata-kickstart.sh https://my-netdata.io/kickstart.sh \
    || curl -fsSL https://my-netdata.io/kickstart.sh -o /tmp/netdata-kickstart.sh
  sudo sh /tmp/netdata-kickstart.sh --stable-channel --disable-telemetry --non-interactive
fi

# The config dir may not exist until the daemon has run once.
sudo mkdir -p "$NDDIR/go.d"

say "2. bind dashboard to localhost only"
# Set [web] bind to = 127.0.0.1 in netdata.conf without disturbing other keys.
NDCONF="$NDDIR/netdata.conf"
if [ ! -f "$NDCONF" ]; then
  # Pull the running config the daemon serves, then edit it.
  sudo sh -c "curl -fsS http://127.0.0.1:19999/netdata.conf > '$NDCONF'" 2>/dev/null || true
fi
if [ -f "$NDCONF" ] && grep -qE '^\s*\[web\]' "$NDCONF"; then
  if grep -qE '^\s*bind to' "$NDCONF"; then
    sudo sed -i -E 's|^\s*bind to.*|	bind to = 127.0.0.1|' "$NDCONF"
  else
    sudo sed -i -E 's|^\s*\[web\]|[web]\n	bind to = 127.0.0.1|' "$NDCONF"
  fi
  echo "  netdata.conf [web] bind to = 127.0.0.1"
else
  # Minimal drop-in if we couldn't fetch the full file.
  printf '[web]\n\tbind to = 127.0.0.1\n' | sudo tee "$NDCONF" >/dev/null
  echo "  wrote minimal netdata.conf binding to 127.0.0.1"
fi

say "3. scrape the app /metrics endpoint (go.d prometheus collector)"
sudo tee "$NDDIR/go.d/prometheus.conf" >/dev/null <<EOF
# Managed by deploy/install-netdata.sh — scrapes the ptr-webapp Go app.
jobs:
  - name: ptr_webapp
    url: $APP_METRICS_URL
    # 1s default scrape; app exposes ptr_http_* + ptr_go_* series.
EOF
echo "  -> $NDDIR/go.d/prometheus.conf (job: ptr_webapp)"

say "4. watch the ptr systemd units (go.d systemdunits collector)"
sudo tee "$NDDIR/go.d/systemdunits.conf" >/dev/null <<'EOF'
# Managed by deploy/install-netdata.sh — surfaces up/down state of our units.
jobs:
  - name: ptr
    include:
      - 'ptr-webapp.service'
      - 'cloudflared.service'
      - 'ptr-import.service'
      - 'ptr-import.timer'
      - 'ptr-backup.service'
      - 'ptr-backup.timer'
EOF
echo "  -> $NDDIR/go.d/systemdunits.conf (ptr-webapp, cloudflared, import, backup)"

say "5. restart Netdata"
sudo systemctl restart netdata
sleep 3
systemctl is-active netdata | sed 's/^/  netdata: /'

say "6. verify"
if curl -fsS http://127.0.0.1:8000/metrics >/dev/null 2>&1; then
  echo "  app /metrics reachable ✔"
else
  echo "  ⚠ app /metrics NOT reachable at $APP_METRICS_URL"
  echo "    (deploy the current Go binary first — the /metrics endpoint ships with it.)"
fi
if curl -fsS "http://127.0.0.1:19999/api/v1/info" >/dev/null 2>&1; then
  echo "  Netdata API up on 127.0.0.1:19999 ✔"
  # Has Netdata picked up our app series yet?
  if curl -fsS "http://127.0.0.1:19999/api/v1/allmetrics?format=prometheus" 2>/dev/null | grep -q 'ptr_webapp'; then
    echo "  Netdata is collecting the ptr_webapp job ✔"
  else
    echo "  (ptr_webapp series not visible yet — give it ~10s, then recheck the dashboard.)"
  fi
else
  echo "  ⚠ Netdata API not responding on 127.0.0.1:19999 — check: journalctl -u netdata -n 40 --no-pager"
fi

cat <<'EONOTE'

== done ==
View the dashboard (it is NOT public — bound to localhost):

  From your PC:   ssh -L 19999:127.0.0.1:19999 alex@ptr1
  Then browse:    http://localhost:19999

To expose it behind Cloudflare Access instead, and to wire up alert
notifications (email / Slack / ntfy), see deploy/MONITORING.md.
EONOTE
