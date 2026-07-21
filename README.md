# server-status

A single Go binary that autodetects a Linux host's metrics and publishes them to MQTT with Home Assistant discovery, so every server appears as its own device. It is a pure state publisher: thresholds and notifications live in Home Assistant (or n8n), not in the agent.

This is the Go rewrite of the original Python `server-status` tool, which is archived at [giovi321/server-status](https://github.com/giovi321/server-status).

**Documentation: https://giovi321.github.io/server-status-go**

## Status

Feature-complete, released as `v1.0.0`. All collectors, MQTT and webhook sinks, the HTTP/MQTT control surface, self-update, and reliability hardening are implemented and validated against a live Home Assistant.

## What it does

From a minimal YAML config, the agent detects what a host can report and publishes only the metrics that have data, so a VPS never shows temperature, GPU, or RAID entities. Each host becomes one MQTT device in Home Assistant with short, human-readable entity names.

Collectors (all auto-detected):

- CPU usage, memory and swap, load average (1/5/15m), and uptime
- Filesystems per real mount: usage, used and total bytes, inode usage, type, and a read-only flag
- Network per interface: rx/tx throughput and link state
- Temperatures from `/sys/class/hwmon` (no lm-sensors dependency)
- APT upgradable and security-update counts, and reboot-required
- systemd failed-unit count and list
- SMART per disk: health, temperature, wear, and identity (ATA and NVMe)
- mdadm arrays, ZFS pools, and NVIDIA GPUs
- Docker inventory plus a non-destructive registry-digest update scan
- Agent last-seen heartbeat and version

Also included:

- **Device hierarchy**: disks, GPUs, RAID arrays, pools, and docker as sub-devices, plus optional host-to-host nesting so a VM appears under its physical machine
- **MQTT and webhook parity**: the same snapshot is POSTed to a webhook, to drive n8n as an alternative to Home Assistant
- **Self-update**: a Home Assistant button (or a control command) pulls the latest release, verifies its checksum, and swaps the binary atomically
- **Control surface**: refresh, restart, and update over HTTP or MQTT, plus a read-only HTTP snapshot endpoint
- **Reliability**: per-collector timeouts and panic isolation, a systemd sd_notify watchdog, and a `--purge` uninstall that removes the host from Home Assistant cleanly

## How it works

Collectors produce a normalized in-memory snapshot of metrics. One or more sinks render that snapshot to a transport. The MQTT sink publishes retained Home Assistant discovery, per-metric state, and an availability LWT, with auto-reconnect; the webhook sink POSTs the same snapshot as JSON. Each collector splits a pure parser (fixture-tested, cross-platform) from a thin Linux reader.

## Requirements

- Debian or Ubuntu Linux (systemd), amd64 or arm64
- The binary is static, with no runtime dependencies. Optional tools are used when present (`smartctl`, `nvidia-smi`, `docker`, `zpool`, and similar)

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/giovi321/server-status-go/main/scripts/install.sh | sudo bash
```

This downloads the right release binary for the host's architecture (checksum-verified), places it in `/opt/server-status/`, writes a default `/etc/server-status/config.yaml` and a chmod-600 `server-status.env` secret file, installs the systemd unit, and starts the service. Set `MQTT_PASSWORD` in `/etc/server-status/server-status.env`, then `sudo systemctl restart server-status`.

To build from source instead (Go 1.22+):

```bash
git clone https://github.com/giovi321/server-status-go.git
cd server-status-go
go build -o server-status ./cmd/server-status
sudo ./scripts/install.sh
```

Preview what a host would publish without connecting to a broker:

```bash
./server-status -c /etc/server-status/config.yaml --dump-detected
```

See the [documentation](https://giovi321.github.io/server-status-go) for the full configuration reference, the metrics catalog, and the Home Assistant, control, and reliability guides.

## Configuration

Minimal config (everything else autodetects):

```yaml
node:                    # short device name; defaults to the hostname
sinks:
  - type: mqtt
    host: 192.168.1.65
    username: mqtt
    password: ${MQTT_PASSWORD}   # interpolated from the environment
```

## Development

Design specs and implementation plans live under `docs/superpowers/`. The documentation site source is under `docs/`. Parsers are unit-tested with fixtures and run on any OS; the collectors that read `/proc` and `/sys` are exercised on Linux.

```bash
go test ./...
go vet ./...
```

## License

See [LICENSE](LICENSE).
