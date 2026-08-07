<p align="center">
  <img src="docs/public/assets/logo.svg" alt="server-status" width="120" />
</p>

<h1 align="center">server-status</h1>

<p align="center">
  <a href="https://github.com/giovi321/server-status-go/actions/workflows/release.yml"><img src="https://github.com/giovi321/server-status-go/actions/workflows/release.yml/badge.svg" alt="Release"></a>
  <a href="https://github.com/giovi321/server-status-go/actions/workflows/docs.yml"><img src="https://github.com/giovi321/server-status-go/actions/workflows/docs.yml/badge.svg" alt="Docs"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT"></a>
  <img src="https://img.shields.io/badge/go-1.26%2B-blue" alt="Go 1.26+">
</p>

<p align="center">
  <a href="https://giovi321.github.io/server-status-go/"><img src="https://img.shields.io/badge/Read_the_docs-2563eb?style=for-the-badge&logo=readthedocs&logoColor=white" alt="Read the documentation"></a>
</p>

If you run more than a couple of Linux boxes, you know the routine: install a monitoring agent on each one, wire up dashboards, and still find out a disk filled up because nobody set an alert for that machine.

server-status is a single static Go binary that ends most of that. Drop it on a host, point it at your MQTT broker, and the server shows up in Home Assistant as its own device with entities for whatever that machine can actually report. A VPS never gets temperature, GPU, or RAID entities it has no data for; a storage box with ZFS and SMART-capable disks gets all of them. Thresholds and notifications live in Home Assistant (or n8n, via the webhook sink), so the agent itself stays a pure state publisher with nothing to tune per host.

It runs on Debian and Ubuntu, amd64 and arm64, with no runtime dependencies. This is the Go rewrite of the original Python `server-status` tool, archived at [giovi321/server-status](https://github.com/giovi321/server-status).

## Quick start

```bash
curl -fsSL https://raw.githubusercontent.com/giovi321/server-status-go/main/scripts/install.sh | sudo bash
```

The installer downloads the right release binary for the host's architecture (checksum-verified), asks for the node name and MQTT broker details, writes `/etc/server-status/config.yaml`, installs the systemd unit, and starts the service. Skip the prompts with `--non-interactive`, preset answers via `NODE_NAME`/`MQTT_HOST`/`MQTT_PORT`/`MQTT_USERNAME`/`MQTT_PASSWORD`, or re-prompt later with `... | sudo bash -s -- --reconfigure`.

Want to see what a host would publish before connecting it to a broker?

```bash
./server-status -c /etc/server-status/config.yaml --dump-detected
```

## What it does

From a minimal YAML config, the agent detects what a host can report and publishes only the metrics that have data. Each host becomes one MQTT device in Home Assistant with short, human-readable entity names.

Collectors, all auto-detected:

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

On top of that:

- **Device hierarchy.** Disks, GPUs, RAID arrays, pools, and Docker appear as sub-devices, with optional host-to-host nesting so a VM shows up under its physical machine.
- **MQTT and webhook parity.** The same snapshot is POSTed to a webhook, so you can drive n8n instead of Home Assistant.
- **Self-update.** A Home Assistant button (or a control command) pulls the latest release, verifies its checksum, and swaps the binary atomically.
- **Control surface.** Refresh, restart, and update over HTTP or MQTT, plus a read-only HTTP snapshot endpoint.
- **Reliability.** Per-collector timeouts and panic isolation, a systemd sd_notify watchdog, and a `--purge` uninstall that removes the host from Home Assistant cleanly.

## How it works

Collectors produce a normalized in-memory snapshot of metrics, and one or more sinks render that snapshot to a transport. The MQTT sink publishes retained Home Assistant discovery, per-metric state, and an availability LWT, with auto-reconnect. The webhook sink POSTs the same snapshot as JSON. Each collector splits a pure parser (fixture-tested, cross-platform) from a thin Linux reader.

## Requirements

- Debian or Ubuntu Linux (systemd), amd64 or arm64
- The binary is static, with no runtime dependencies. Optional tools are used when present (`smartctl`, `nvidia-smi`, `docker`, `zpool`, and similar)

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

See the [documentation](https://giovi321.github.io/server-status-go) for the full configuration reference, the metrics catalog, and the Home Assistant, control, and reliability guides.

## Building from source

Requires Go 1.26+, per the `go` directive in `go.mod`.

```bash
git clone https://github.com/giovi321/server-status-go.git
cd server-status-go
go build -o server-status ./cmd/server-status
sudo ./scripts/install.sh
```

## Development

Design specs and implementation plans live under `docs/superpowers/`. The documentation site source is under `docs/`. Parsers are unit-tested with fixtures and run on any OS; the collectors that read `/proc` and `/sys` are exercised on Linux.

```bash
go test ./...
go vet ./...
```

## License

Released under the [MIT License](LICENSE).
