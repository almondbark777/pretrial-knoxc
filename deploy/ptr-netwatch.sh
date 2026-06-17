#!/bin/sh
# ptr-netwatch.sh — network watchdog for ptr1's flaky building-WiFi uplink.
# Run every minute by ptr-netwatch.timer. Pings out; after N consecutive
# failures it bounces the Wi-Fi interface, kicks cloudflared, and ntfy-alerts.
# Sends a recovery alert when connectivity returns. Install at
# /usr/local/bin/ptr-netwatch.sh (root, 0755).
#
# CONTEXT: ptr1's uplink is the building WiFi via a USB adapter (wlx*); the wired
# port (enp1s0) is unplugged. Most outages observed are the UPSTREAM losing its
# route while the local carrier stays up ("network is unreachable" with carrier
# present, DNS failing) — e.g. the 2026-06-17 ~13:52-14:00 UTC tunnel outage.
# Bouncing the local interface CANNOT fix an upstream/building-WiFi WAN drop, but
# it (a) covers local carrier/DHCP-lease losses, (b) forces cloudflared to
# re-dial immediately instead of sitting in its backoff, and (c) tells you it
# happened. The durable fix is a wired ethernet connection.
set -eu

PING_HOSTS="${PING_HOSTS:-1.1.1.1 8.8.8.8}"     # online = at least one replies
DNS_CHECK_HOST="${DNS_CHECK_HOST:-cloudflare.com}"
FAIL_THRESHOLD="${FAIL_THRESHOLD:-3}"           # consecutive 1-min failures before acting
RENOTIFY_EVERY="${RENOTIFY_EVERY:-15}"          # re-alert every N min while still down
WIFI_IF="${WIFI_IF:-}"                          # autodetected from default route if empty
STATE="/run/ptr-netwatch.fails"
. /etc/ptr-alert.env                            # provides NTFY_URL

alert(){ curl -fsS -m 15 -H "Title: $1" -H "Priority: $2" -H "Tags: $3" -d "$4" "$NTFY_URL" >/dev/null 2>&1 || true; }

# Autodetect the default-route interface unless pinned via env.
if [ -z "$WIFI_IF" ]; then
  WIFI_IF="$(ip route show default 2>/dev/null | awk '/default/{print $5; exit}')"
fi

# Connectivity probe: at least one host must answer AND DNS must resolve
# (the canonical outage failed on both reachability and name lookup).
online=0
for h in $PING_HOSTS; do
  if ping -c1 -W2 "$h" >/dev/null 2>&1; then online=1; break; fi
done
if [ "$online" -eq 1 ] && ! getent hosts "$DNS_CHECK_HOST" >/dev/null 2>&1; then
  online=0   # link works but DNS is dead — cloudflared still can't connect
fi

fails="$(cat "$STATE" 2>/dev/null || echo 0)"

if [ "$online" -eq 1 ]; then
  if [ "$fails" -ge "$FAIL_THRESHOLD" ]; then
    alert "ptr1 network RECOVERED" default white_check_mark "Connectivity restored on ${WIFI_IF:-?} after ${fails} min down."
  fi
  echo 0 > "$STATE"
  exit 0
fi

fails=$(( fails + 1 ))
echo "$fails" > "$STATE"

# Below threshold: transient blip, wait it out.
[ "$fails" -lt "$FAIL_THRESHOLD" ] && exit 0

# Exactly at threshold: attempt local recovery once (don't bounce every minute
# during a long upstream outage — that just churns).
if [ "$fails" -eq "$FAIL_THRESHOLD" ]; then
  alert "ptr1 network DOWN" high rotating_light "No connectivity for ${fails} min (iface ${WIFI_IF:-unknown}). Bouncing Wi-Fi + restarting cloudflared."
  if [ -n "$WIFI_IF" ]; then
    ip link set "$WIFI_IF" down 2>/dev/null || true
    sleep 3
    ip link set "$WIFI_IF" up 2>/dev/null || true
    networkctl reconfigure "$WIFI_IF" 2>/dev/null || systemctl restart systemd-networkd 2>/dev/null || true
  fi
  systemctl restart cloudflared 2>/dev/null || true
  exit 0
fi

# Still down well past threshold: periodic reminder; local recovery already tried.
if [ "$(( fails % RENOTIFY_EVERY ))" -eq 0 ]; then
  alert "ptr1 still DOWN" high rotating_light "Network still down after ${fails} min on ${WIFI_IF:-unknown}. Likely an upstream/building-WiFi outage — local recovery can't fix that."
fi
exit 0
