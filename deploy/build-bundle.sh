#!/usr/bin/env bash
# build-bundle.sh — assemble deploy/dist/ptr1-deploy.tar.gz on the DEV box.
# Cross-compiles the Linux server binary (pure-Go SQLite → no CGO, no toolchain
# on ptr1) and stages it with templates/, static/, and every installer/unit/doc
# ptr1 needs. Run from anywhere; it resolves the repo root itself:
#
#   bash deploy/build-bundle.sh
#
# Then:
#   scp deploy/dist/ptr1-deploy.tar.gz alex@ptr1:~
#   ssh alex@ptr1 'tar xzf ptr1-deploy.tar.gz && cd ptr1-deploy && ./install-all.sh'
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
STAGE="deploy/dist/ptr1-deploy"
OUT="deploy/dist/ptr1-deploy.tar.gz"

echo "== 1. cross-compile server (linux/amd64, CGO off) =="
rm -rf "$STAGE" "$OUT"
mkdir -p "$STAGE"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$STAGE/server" ./cmd/server
ls -la "$STAGE/server" | sed 's/^/  /'

echo "== 2. stage templates/ + static/ =="
cp -r templates static "$STAGE/"

echo "== 3. stage installers + units + docs (flat, beside the binary) =="
for f in \
  deploy/install-all.sh deploy/install-on-ptr1.sh deploy/install-netdata.sh \
  deploy/smoke.sh deploy/ptr-webapp-go.service \
  deploy/ptr-backup.sh deploy/ptr-backup.service deploy/ptr-backup.timer \
  deploy/MONITORING.md deploy/DEPLOY_GO.md deploy/cloudflared-config.example.yml
do
  cp "$f" "$STAGE/"
done
chmod +x "$STAGE"/*.sh

echo "== 4. tar =="
tar -czf "$OUT" -C deploy/dist ptr1-deploy
echo "  wrote $OUT ($(du -h "$OUT" | cut -f1))"

echo
echo "== bundle root contents =="
tar -tzf "$OUT" | grep -E '^ptr1-deploy/[^/]+$' | sed 's/^/  /'

echo
echo "Next:"
echo "  scp $OUT alex@ptr1:~"
echo "  ssh alex@ptr1 'tar xzf ptr1-deploy.tar.gz && cd ptr1-deploy && ./install-all.sh'"
