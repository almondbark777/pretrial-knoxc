#!/usr/bin/env bash
# dev_bootstrap.sh — set up the Go + Node toolchain for pretrial-knoxc dev work.
#
# Idempotent. Safe to re-run. Installs Go to ~/.local/go (no root needed) and
# verifies Node. Used to re-provision an ephemeral build sandbox each session,
# but works on any Linux amd64 box.
#
#   bash tools/dev_bootstrap.sh
#   source tools/dev_bootstrap.sh   # to also load PATH into the current shell
set -euo pipefail

GO_VERSION="${GO_VERSION:-go1.26.3}"      # pin; bump deliberately
GO_ROOT="$HOME/.local/go"
ARCH="$(dpkg --print-architecture 2>/dev/null || uname -m)"
[ "$ARCH" = "x86_64" ] && ARCH="amd64"

echo "== Go =="
if "$GO_ROOT/bin/go" version 2>/dev/null | grep -q "$GO_VERSION"; then
  echo "  $GO_VERSION already installed at $GO_ROOT"
else
  tarball="${GO_VERSION}.linux-${ARCH}.tar.gz"
  echo "  downloading $tarball ..."
  curl -fsSL -o "/tmp/$tarball" "https://go.dev/dl/$tarball"
  mkdir -p "$HOME/.local"
  rm -rf "$GO_ROOT"
  tar -C "$HOME/.local" -xzf "/tmp/$tarball"
  rm -f "/tmp/$tarball"
  echo "  installed $("$GO_ROOT/bin/go" version)"
fi

# Persist PATH for login/interactive shells (non-interactive `bash -c` won't
# source these, so Go commands in scripts should prefix PATH themselves).
if ! grep -q 'pretrial-knoxc go toolchain' "$HOME/.profile" 2>/dev/null; then
  cat >> "$HOME/.profile" <<'EOF'

# pretrial-knoxc go toolchain
export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"
export GOPATH="$HOME/go"
EOF
  echo "  appended PATH/GOPATH to ~/.profile"
fi

echo "== Node =="
if command -v node >/dev/null 2>&1; then
  echo "  node $(node --version), npm $(npm --version)"
else
  echo "  WARNING: node not found. Install Node 20+ (nvm or distro package)."
fi

echo "== Summary =="
export PATH="$GO_ROOT/bin:$HOME/go/bin:$PATH"
export GOPATH="${GOPATH:-$HOME/go}"
go version
echo "GOPATH=$GOPATH"
echo
echo "For non-interactive shells, prefix Go commands with:"
echo '  export PATH="$HOME/.local/go/bin:$PATH"'
