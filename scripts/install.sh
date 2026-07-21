#!/usr/bin/env bash
# Install, upgrade, or uninstall server-status. Self-contained: works from a
# git checkout, from a bare downloaded binary, or with no local files at all.
#
# Usage:
#   sudo ./install.sh                 install/upgrade
#   sudo ./install.sh --uninstall     stop the service, purge HA discovery, remove the unit
#
# One-liner (no clone, no manual download):
#   curl -fsSL https://raw.githubusercontent.com/giovi321/server-status-go/main/scripts/install.sh | sudo bash
#
# Binary resolution order:
#   1. $SRC_BIN, if set
#   2. ./server-status
#   3. ./server-status-linux-<arch>   (the release asset, as downloaded, no rename needed)
#   4. downloaded from the $REPO release tagged $VERSION (default: latest), checksum-verified
set -euo pipefail

REPO=${REPO:-giovi321/server-status-go}
VERSION=${VERSION:-latest}
BIN_DIR=/opt/server-status
CFG_DIR=/etc/server-status
UNIT=/etc/systemd/system/server-status.service

if [[ "${1:-}" == "--uninstall" ]]; then
  systemctl disable --now server-status.service 2>/dev/null || true
  if [[ -x "$BIN_DIR/server-status" && -f "$CFG_DIR/config.yaml" ]]; then
    if [[ -f "$CFG_DIR/server-status.env" ]]; then set -a; . "$CFG_DIR/server-status.env"; set +a; fi
    "$BIN_DIR/server-status" -c "$CFG_DIR/config.yaml" --purge 2>/dev/null || true
  fi
  rm -f "$UNIT"
  systemctl daemon-reload
  echo "Uninstalled service and cleared Home Assistant discovery. Left $CFG_DIR and $BIN_DIR in place."
  exit 0
fi

case "$(uname -m)" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
ASSET="server-status-linux-$ARCH"

resolve_binary() {
  if [[ -n "${SRC_BIN:-}" ]]; then
    [[ -f "$SRC_BIN" ]] || { echo "SRC_BIN=$SRC_BIN not found" >&2; exit 1; }
    printf '%s\n' "$SRC_BIN"
    return
  fi
  if [[ -f "./server-status" ]]; then printf '%s\n' "./server-status"; return; fi
  if [[ -f "./$ASSET" ]]; then printf '%s\n' "./$ASSET"; return; fi

  command -v curl >/dev/null || { echo "no local binary found and curl is missing to download one; install curl or set SRC_BIN" >&2; exit 1; }
  local base tmpdir
  if [[ "$VERSION" == "latest" ]]; then
    base="https://github.com/$REPO/releases/latest/download"
  else
    base="https://github.com/$REPO/releases/download/$VERSION"
  fi
  tmpdir=$(mktemp -d)
  echo "no local binary found; downloading $ASSET ($VERSION) from $REPO" >&2
  curl -fL -o "$tmpdir/$ASSET" "$base/$ASSET"
  curl -fL -o "$tmpdir/$ASSET.sha256" "$base/$ASSET.sha256"
  ( cd "$tmpdir" && sha256sum -c "$ASSET.sha256" ) >&2
  chmod +x "$tmpdir/$ASSET"
  printf '%s\n' "$tmpdir/$ASSET"
}

SRC=$(resolve_binary)

install -d "$BIN_DIR" "$CFG_DIR"
install -m 0755 "$SRC" "$BIN_DIR/server-status"

if [[ ! -f "$CFG_DIR/config.yaml" ]]; then
  cat > "$CFG_DIR/config.yaml" <<'YAML'
# Minimal config. See docs for all options.
node:            # defaults to hostname
sinks:
  - type: mqtt
    host: 192.168.1.65
    username: mqtt
    password: ${MQTT_PASSWORD}
YAML
  echo "Wrote default config to $CFG_DIR/config.yaml (edit it, then restart the service)."
fi

if [[ ! -f "$CFG_DIR/server-status.env" ]]; then
  cat > "$CFG_DIR/server-status.env" <<'ENVEOF'
# Secrets for server-status, loaded into the service environment by systemd
# (EnvironmentFile in the unit). Referenced from config.yaml as ${MQTT_PASSWORD}.
MQTT_PASSWORD=
ENVEOF
  chmod 600 "$CFG_DIR/server-status.env"
  echo "Wrote secret stub to $CFG_DIR/server-status.env (set MQTT_PASSWORD there; it is chmod 600)."
fi

# Kept identical to packaging/server-status.service so the script has no other file dependency.
cat > "$UNIT" <<'UNITEOF'
[Unit]
Description=server-status metrics publisher
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
WatchdogSec=180
User=root
EnvironmentFile=-/etc/server-status/server-status.env
ExecStart=/opt/server-status/server-status -c /etc/server-status/config.yaml
Restart=always
RestartSec=15s
TimeoutStopSec=10s
# Sandboxing that does not block hardware access; expanded in a later reliability plan.
ProtectHome=true
NoNewPrivileges=false

[Install]
WantedBy=multi-user.target
UNITEOF
chmod 0644 "$UNIT"

systemctl daemon-reload
systemctl enable server-status.service
systemctl restart server-status.service
echo "Installed server-status. Set MQTT_PASSWORD in $CFG_DIR/server-status.env, then: systemctl restart server-status. Logs: journalctl -u server-status -f"
