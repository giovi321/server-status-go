# server-status

A single Go binary that autodetects a Linux host's metrics and publishes them to MQTT with Home Assistant discovery, so every server appears as its own device. It is a pure state publisher: thresholds and notifications live in Home Assistant (or n8n), not in the agent.

This is the Go rewrite of the original Python `server-status` tool, which is archived at [giovi321/server-status](https://github.com/giovi321/server-status).

## Status

Under active development. The foundation and the core auto-detected collectors are done and validated against a live Home Assistant. Storage health, GPU, docker, webhook parity, and the self-update button are on the roadmap below.

## What it does

From a minimal YAML config, the agent detects what a host can report and publishes only sensors that have data, so a VPS never shows temperature, GPU, or RAID entities. Each host becomes one MQTT device in Home Assistant with short, human-readable entity names.

Collectors available today (all auto-detected):

- CPU usage and temperature (read from `/sys/class/hwmon`, no lm-sensors dependency)
- Memory and swap usage
- Load average (1/5/15m) and uptime
- Filesystems per real mount: usage (df-style), used and total bytes, inode usage, filesystem type, and a read-only flag, with pseudo and network-mount hardening
- Network per interface: rx/tx throughput and link state
- APT upgradable count, security-update count, and reboot-required
- systemd failed-unit count and list

Planned (see `docs/superpowers/`):

- Device hierarchy: disks, GPUs, RAID arrays, and docker as sub-devices, and optional host-to-host nesting so VMs appear under their physical machine
- Storage health: SMART attributes and disk serials, mdadm, ZFS, btrfs
- GPU (nvidia) and a non-destructive docker update scan (registry digest compare)
- Webhook parity alongside MQTT (to drive n8n as an alternative to Home Assistant)
- A Home Assistant button to self-update the agent, backed by a GitHub Releases pipeline
- Reliability hardening (systemd watchdog, cached slow collectors)

## How it works

Collectors produce a normalized in-memory snapshot of metrics. A sink renders that snapshot to a transport. Today the MQTT sink publishes retained Home Assistant discovery, per-metric state, and an availability LWT, with auto-reconnect. Each collector splits a pure parser (fixture-tested, cross-platform) from a thin Linux reader.

## Requirements

- Debian or Ubuntu Linux (systemd)
- The binary is static; no runtime dependencies. Optional tools are used when present (`smartctl`, `nvidia-smi`, docker, and similar are used by later collectors)

## Install

Build from source (needs Go 1.22+):

```bash
go build -o server-status ./cmd/server-status
sudo ./scripts/install.sh
```

The installer places the binary in `/opt/server-status/`, writes a default `/etc/server-status/config.yaml` and a chmod-600 `server-status.env` secret file, installs the systemd unit, and starts the service. Set `MQTT_PASSWORD` in `/etc/server-status/server-status.env`, then `sudo systemctl restart server-status`.

Preview what a host would publish without connecting to a broker:

```bash
./server-status -c /etc/server-status/config.yaml --dump-detected
```

## Configuration

Minimal config (everything else autodetects):

```yaml
node: myhost            # short device name; defaults to the hostname
sinks:
  - type: mqtt
    host: 192.168.1.65
    username: mqtt
    password: ${MQTT_PASSWORD}   # interpolated from the environment
```

## Home Assistant

Each host is published as a single MQTT device via Home Assistant discovery (default prefix `homeassistant`). Entities use `has_entity_name`, so names stay short (for example a device `myhost` with a `CPU usage` entity, `sensor.myhost_cpu_usage`). Serial numbers and other identifiers are kept out of display names.

## Development

Design specs and implementation plans live under `docs/superpowers/`. Parsers are unit-tested with fixtures and run on any OS; the collectors that read `/proc` and `/sys` are exercised on Linux.

```bash
go test ./...
go vet ./...
```

## License

See [LICENSE](LICENSE).
