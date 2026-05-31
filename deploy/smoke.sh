#!/usr/bin/env bash
# Post-deploy smoke test for the Knox County Pre-Trial Go app.
#
# Run ON ptr1 (or anywhere that can reach the app). On-box requests don't carry
# the Cloudflare Access header, so this uses HTTP Basic auth (the app's fallback):
# an allow-listed @knoxsheriff.org email + APP_PASSWORD. Use a SUPERVISOR email so
# the supervisor pages return 200.
#
# Usage:
#   BASE=http://127.0.0.1:8000 EMAIL=you@knoxsheriff.org PW=secret bash deploy/smoke.sh
set -uo pipefail

BASE="${BASE:-http://127.0.0.1:8000}"
EMAIL="${EMAIL:?set EMAIL to an allow-listed (supervisor) @knoxsheriff.org address}"
PW="${PW:?set PW to the APP_PASSWORD}"
AUTH=(-u "$EMAIL:$PW")
fail=0
ok()  { printf '  \033[32mok\033[0m   %s\n' "$1"; }
bad() { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fail=1; }

# code METHOD PATH WANT [curl-extra...]
chk() {
  local method="$1" path="$2" want="$3"; shift 3
  local code
  code=$(curl -s -o /dev/null -w '%{http_code}' -X "$method" "${AUTH[@]}" "$@" "$BASE$path")
  if [ "$code" = "$want" ]; then ok "$method $path -> $code"; else bad "$method $path -> $code (want $want)"; fi
}

echo "Smoke test: $BASE  (as $EMAIL)"

# 1. /health is auth-free and reports ok.
hc=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/health")
[ "$hc" = 200 ] && ok "/health (auth-free) -> 200" || bad "/health -> $hc"
curl -s "$BASE/health" | grep -q '"ok":true' && ok '/health body ok:true' || bad "/health body not ok:true"

# 2. Read pages load (Basic auth) -> 200.
for p in / /dashboard /my_day.html /pretrial_app.html /analytics.html /calendar.html /admin/deleted /admin/audit; do
  chk GET "$p" 200
done

# 3. Data feed + CSV exports.
chk GET /api/lookup_data 200
for p in /export/behind.csv /export/missed.csv /export/cases.csv; do chk GET "$p" 200; done

# 4. CSRF: an admin POST with no token must be rejected.
chk POST /admin/note/add 403 -d "idn=1&body=smoke"

# 5. Auth gate: an UNauthenticated protected page must not be 200.
uc=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/dashboard")
[ "$uc" != 200 ] && ok "/dashboard unauthenticated -> $uc (gated)" || bad "/dashboard unauthenticated returned 200!"

echo
if [ "$fail" = 0 ]; then echo "PASS — app is healthy."; else echo "FAIL — see above."; exit 1; fi
