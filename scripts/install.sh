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
  if [[ -x "$BIN_DIR/server-status" && -f "$CFG_DIR/config.yaml" ]]; then
    if [[ -f "$CFG_DIR/server-status.env" ]]; then set -a; . "$CFG_DIR/server-status.env"; set +a; fi
    "$BIN_DIR/server-status" -c "$CFG_DIR/config.yaml" --purge 2>/dev/null || true
  fi
  rm -f "$UNIT"
  systemctl daemon-reload
  echo "Uninstalled service and cleared Home Assistant discovery. Left $CFG_DIR and $BIN_DIR in place."
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

if [[ ! -f "$CFG_DIR/server-status.env" ]]; then
  cat > "$CFG_DIR/server-status.env" <<'ENVEOF'
# Secrets for server-status, loaded into the service environment by systemd
# (EnvironmentFile in the unit). Referenced from config.yaml as ${MQTT_PASSWORD}.
MQTT_PASSWORD=
ENVEOF
  chmod 600 "$CFG_DIR/server-status.env"
  echo "Wrote secret stub to $CFG_DIR/server-status.env (set MQTT_PASSWORD there; it is chmod 600)."
fi

install -m 0644 packaging/server-status.service "$UNIT"
systemctl daemon-reload
systemctl enable server-status.service
systemctl restart server-status.service
echo "Installed server-status. Set MQTT_PASSWORD in $CFG_DIR/server-status.env, then: systemctl restart server-status. Logs: journalctl -u server-status -f"
