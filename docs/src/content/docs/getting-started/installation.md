---
title: "Installation"
description: "Install server-status as a systemd service on a Linux host"
---

server-status runs on each host you want to monitor. It is a single static binary with no runtime dependencies. This page covers installing it as a systemd service; to try it without installing anything, see [Running the agent](../running/).

## Prerequisites

- A **Linux host** (built and tested for Debian and Ubuntu, amd64 or arm64). It reads `/proc` and `/sys` and shells out to a few standard tools when present (`smartctl`, `docker`, `nvidia-smi`, `zpool`, and so on)
- An **MQTT broker** reachable from the host (for the Home Assistant integration). A webhook-only setup needs no broker
- Either a **release binary** (recommended) or **Go 1.22+** to build from source

:::note[Where thresholds live]
server-status only publishes state. Alerting and automation belong in Home Assistant or whatever consumes the webhook. There is nothing to configure on the agent for notifications.
:::

## Install as a service

`scripts/install.sh` does everything: it resolves a binary (downloading one if needed), installs it, writes a starter config, installs the systemd unit, and starts the service. It needs no other file from the repo, so it can run standalone with nothing pre-downloaded:

```bash
curl -fsSL https://raw.githubusercontent.com/giovi321/server-status-go/main/scripts/install.sh | sudo bash
```

The same script is also attached to every [release](https://github.com/giovi321/server-status-go/releases) as `install.sh`, so `https://github.com/giovi321/server-status-go/releases/latest/download/install.sh` works too.

It resolves the binary to install in this order:

1. `$SRC_BIN`, if set
2. `./server-status`
3. `./server-status-linux-<arch>` — the release asset exactly as downloaded, no rename needed
4. otherwise it downloads `server-status-linux-<arch>` for the detected architecture from the latest GitHub release (or `$VERSION`, e.g. `VERSION=v1.0.0`), verifying its `.sha256` checksum

So if you already fetched a binary by hand or built from source, drop it next to `install.sh` (or point `SRC_BIN` at it) and it's used as-is; otherwise the script fetches and verifies one itself.

It performs these steps:

| Step | Path | Notes |
|------|------|-------|
| Install binary | `/opt/server-status/server-status` | mode 0755 |
| Write starter config (if absent) | `/etc/server-status/config.yaml` | edit before relying on it |
| Write secret stub (if absent) | `/etc/server-status/server-status.env` | mode 0600, holds `MQTT_PASSWORD` |
| Install unit | `/etc/systemd/system/server-status.service` | `Type=notify`, watchdog enabled |
| Enable + start | | `systemctl enable --now` |

### Building from source instead

```bash
git clone https://github.com/giovi321/server-status-go.git
cd server-status-go
go build -o server-status ./cmd/server-status
sudo ./scripts/install.sh
```

## Configure

The starter config is minimal. Edit `/etc/server-status/config.yaml` to point at your broker, then put the broker password in the environment file so it never lands in the config:

```yaml
# /etc/server-status/config.yaml
node:            # defaults to the hostname
sinks:
  - type: mqtt
    host: 192.168.1.65
    username: mqtt
    password: ${MQTT_PASSWORD}
```

```bash
# /etc/server-status/server-status.env  (mode 0600)
MQTT_PASSWORD=your-broker-password
```

`${VAR}` references in the config are expanded from the environment, and systemd loads the environment file into the service. See [Configuration](../configuration/) for the starter walk-through and the [Configuration reference](../../reference/config/) for every option.

Apply changes with:

```bash
sudo systemctl restart server-status
```

## Verify

```bash
# Service state and recent logs
systemctl status server-status
journalctl -u server-status -f

# What this host detects and would publish (no broker needed)
/opt/server-status/server-status -c /etc/server-status/config.yaml --dump-detected
```

Within one cycle (default 60 seconds) the host appears in Home Assistant as a device named after its node. If it does not, check the logs for MQTT connection errors and confirm the broker host, credentials, and firewall.

## Upgrade

Once a release is available, the agent can update itself: press the **Update** button on the host's device in Home Assistant, or send the `update` control command. See [Self-update](../../control/self-update/). To upgrade manually, replace `/opt/server-status/server-status` with the new binary (or re-run `install.sh`) and restart the service.

## Uninstall

```bash
sudo ./scripts/install.sh --uninstall
```

This stops the service, runs `server-status --purge` to clear the host's retained MQTT discovery (so the Home Assistant device disappears cleanly), and removes the systemd unit. It leaves `/etc/server-status` and `/opt/server-status` in place; delete them by hand to remove the config and binary too. See [Clean uninstall](../../reliability/purge/) for what `--purge` does.
