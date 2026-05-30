#!/usr/bin/env bash
#
# setup.sh — one-shot installer for the Knox County Pre-Trial web app on a
# Debian/Ubuntu Linux server.
#
# What it does (the automatable parts):
#   1. Installs OS packages: python venv/pip, cloudflared
#   2. Creates a dedicated service user `ptrapp`
#   3. Copies the app to /opt/ptr-knoxc and builds a virtualenv
#   4. Installs the systemd service (uvicorn bound to 127.0.0.1:8000)
#
# What it deliberately does NOT do (needs you / a browser):
#   - Fill in webapp/.env secrets
#   - `cloudflared tunnel login` / create / DNS route   (browser auth)
#   - Create the Cloudflare Access policy
#   See DEPLOY_GUIDE.md for those steps.
#
# Run it from inside the extracted package directory, as root:
#   sudo bash deploy/setup.sh
#
set -euo pipefail

APP_DIR=/opt/ptr-knoxc
SERVICE_USER=ptrapp
PORT=8000

say() { printf '\n\033[1;36m==> %s\033[0m\n' "$*"; }
warn() { printf '\n\033[1;33m!!  %s\033[0m\n' "$*"; }

if [[ $EUID -ne 0 ]]; then
  echo "Please run with sudo: sudo bash deploy/setup.sh" >&2
  exit 1
fi

# Resolve the package root (parent of this script's deploy/ dir).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PKG_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
if [[ ! -d "$PKG_ROOT/webapp" ]]; then
  echo "Could not find webapp/ next to deploy/. Run from the extracted package." >&2
  exit 1
fi

say "1/5  Installing OS packages"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y python3-venv python3-pip unzip curl ca-certificates lsb-release

say "2/5  Installing cloudflared (direct .deb — works on any Ubuntu/Debian)"
if ! command -v cloudflared >/dev/null 2>&1; then
  ARCH=$(dpkg --print-architecture)
  curl -fsSL "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-${ARCH}.deb" -o /tmp/cloudflared.deb
  apt-get install -y /tmp/cloudflared.deb || dpkg -i /tmp/cloudflared.deb
else
  echo "cloudflared already present: $(cloudflared --version 2>/dev/null | head -1)"
fi

say "3/5  Creating service user '$SERVICE_USER' and app dir"
if ! id "$SERVICE_USER" >/dev/null 2>&1; then
  useradd --system --create-home --shell /usr/sbin/nologin "$SERVICE_USER"
fi
mkdir -p "$APP_DIR"

say "4/5  Copying app to $APP_DIR and building virtualenv"
# Copy webapp + db reference (schema/migrations) into place.
cp -r "$PKG_ROOT/webapp" "$APP_DIR/"
[[ -d "$PKG_ROOT/db" ]] && cp -r "$PKG_ROOT/db" "$APP_DIR/"
[[ -d "$PKG_ROOT/tools" ]] && cp -r "$PKG_ROOT/tools" "$APP_DIR/"

python3 -m venv "$APP_DIR/venv"
"$APP_DIR/venv/bin/pip" install --upgrade pip
"$APP_DIR/venv/bin/pip" install -r "$APP_DIR/webapp/requirements.txt"

# Seed .env from the example if it isn't there yet, and lock it down.
if [[ ! -f "$APP_DIR/webapp/.env" ]]; then
  cp "$APP_DIR/webapp/.env.example" "$APP_DIR/webapp/.env"
  warn ".env created from template — YOU MUST EDIT IT (APP_PASSWORD, APP_SESSION_SECRET)."
fi
chmod 600 "$APP_DIR/webapp/.env"
chown -R "$SERVICE_USER:$SERVICE_USER" "$APP_DIR"

say "5/5  Installing systemd service"
cp "$PKG_ROOT/deploy/ptr-webapp.service" /etc/systemd/system/ptr-webapp.service
systemctl daemon-reload
systemctl enable ptr-webapp.service

cat <<EOF

Base install complete.  Next steps (see DEPLOY_GUIDE.md):

  1. Edit secrets:        sudo nano $APP_DIR/webapp/.env
       - APP_PASSWORD         (shared office login password)
       - APP_SESSION_SECRET   (run: openssl rand -hex 32)
  2. Start the app:        sudo systemctl start ptr-webapp
       Verify locally:     curl -s http://127.0.0.1:$PORT/health
  3. Set up the tunnel:    cloudflared tunnel login
                           cloudflared tunnel create ptr-knoxc
                           cloudflared tunnel route dns ptr-knoxc ptr.<yourdomain>
       Put config at /etc/cloudflared/config.yml (see cloudflared-config.example.yml)
       then: sudo cloudflared service install && sudo systemctl start cloudflared
  4. Lock it down with Cloudflare Access (login gate) — DEPLOY_GUIDE.md Part G.

EOF
