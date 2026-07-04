#!/usr/bin/env bash
# Install or upgrade server-status from a locally built binary.
# Usage: sudo ./scripts/install.sh [--uninstall]
set -euo pipefail

BIN_DIR=/opt/server-status
CFG_DIR=/etc/server-status
UNIT=/etc/systemd/system/server-status.service
SRC_BIN=${SRC_BIN:-./server-status}

if [[ "${1:-}" == "--uninstall" ]]; then
  systemctl disable --now server-status.service 2>/dev/null || true
  rm -f "$UNIT"
  systemctl daemon-reload
  echo "Uninstalled service. Left $CFG_DIR and $BIN_DIR in place."
  exit 0
fi

if [[ ! -f "$SRC_BIN" ]]; then
  echo "Binary not found at $SRC_BIN. Build it first: go build -o server-status ./cmd/server-status" >&2
  exit 1
fi

install -d "$BIN_DIR" "$CFG_DIR"
install -m 0755 "$SRC_BIN" "$BIN_DIR/server-status"

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

install -m 0644 packaging/server-status.service "$UNIT"
systemctl daemon-reload
systemctl enable --now server-status.service
echo "Installed and started server-status. Logs: journalctl -u server-status -f"
