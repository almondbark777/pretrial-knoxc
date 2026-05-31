#!/usr/bin/env bash
# install-all.sh — one-shot ptr1 setup: deploy the Go app, then stand up
# monitoring. Run ON ptr1 as `alex`, from the extracted bundle:
#
#   tar xzf ptr1-deploy.tar.gz && cd ptr1-deploy && ./install-all.sh
#
# It simply chains the two installers in the right order and stops if the app
# step fails (no point monitoring a broken deploy). Both sub-scripts are safe +
# re-runnable, so re-running this is fine.
set -euo pipefail

SRC="$(cd "$(dirname "$0")" && pwd)"
banner(){ printf '\n\033[1;36m######## %s ########\033[0m\n' "$*"; }

banner "STEP 1/2 — app: swap in the Go binary + templates + service"
bash "$SRC/install-on-ptr1.sh"

banner "STEP 2/2 — monitoring: Netdata + scrape app /metrics + phone alerts"
bash "$SRC/install-netdata.sh"

banner "ALL DONE"
cat <<'EONOTE'
Remaining (one-time, off-box):
  1. Remote dashboard — add the netdata-ptr.<domain> hostname to your cloudflared
     ingress + a Cloudflare Access app with the SAME email-allowlist policy.
     Full steps: MONITORING.md  (cloudflared-config.example.yml has the block).
  2. (Optional) External /health uptime monitor — see MONITORING.md, Layer 3.

Not done by this script (still the open backup gap): automated DB backups —
ptr-backup.{sh,service,timer} are in this bundle; ask Claude to wire them in next.
EONOTE
