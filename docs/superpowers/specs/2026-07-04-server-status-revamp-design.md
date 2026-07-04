# server-status revamp design

Status: approved for planning
Date: 2026-07-04
Author: giovi321 (with Claude)
Supersedes: the current single-file `server-status.py`

## 1. Summary

Rewrite `server-status` as a single Go static binary that runs one instance per host, autodetects what the host can report, publishes a normalized state snapshot to any combination of sinks (MQTT for Home Assistant, Webhook for n8n), and accepts privileged control commands (self-update, refresh) over both MQTT and HTTP. The agent is a pure state publisher: it holds no thresholds and sends no notifications itself. Home Assistant and, later, n8n own all thresholds and notifications.

The rewrite exists because the current 776-line Python script cannot cleanly meet the goals of aggressive autoconfiguration, one-file deploy and upgrade, webhook/MQTT parity, a self-update button, a non-destructive docker update scan, per-host device separation, a parent-child device hierarchy, and a much richer hardware and software inventory with human-readable Home Assistant names.

## 2. Goals

- Autoconfigure aggressively: publish only sensors that have real data on this host, so a VPS never shows temperature, GPU, or RAID entities
- One static binary, deploy by copying a file plus a systemd unit, upgrade by replacing that file
- Reliable: survive broker restarts, network drops, slow or failing probes, and its own hangs
- Controllable: a Home Assistant button (and an HTTP endpoint) to self-update, and to force a refresh
- Webhook and MQTT parity by construction, so moving from Home Assistant to n8n is a config change, not a code change
- Every server appears as its own MQTT device in Home Assistant
- Optional parent-child device hierarchy, both host-to-host (VMs nested under their physical machine) and host-to-component (disks, GPUs, arrays, docker as sub-devices of the host)
- A much larger hardware and software inventory (disk serials, SMART attributes, and more)
- Consistent, hierarchical, short, human-readable Home Assistant names, with serials and UUIDs kept out of display names

## 3. Non-goals

- No agent-side thresholds, alerting, or notifications (owned by Home Assistant and n8n)
- No non-Debian/Ubuntu Linux and no non-Linux hosts in this version
- No web UI or dashboard shipped with the agent (Home Assistant is the UI)
- No pull-based docker update detection (registry digest compare only, never pulls)
- No host-side enumeration of guest VMs in this version (each VM declares its parent instead)

## 4. Locked decisions

These were decided during brainstorming and are fixed for this spec.

| Topic | Decision |
|-------|----------|
| Runtime and packaging | Go single static binary plus systemd unit |
| Fleet and OS | Debian/Ubuntu only |
| Alerting ownership | Agent exposes data only; Home Assistant and n8n own thresholds and notifications |
| Docker update detection | Registry digest compare, no pull |
| Outbound parity model | One normalized snapshot rendered to pluggable sinks |
| Self-update delivery | Download from GitHub Releases, verify checksum, atomic swap, systemd restart |
| Control surface | MQTT command topics and an HTTP control endpoint, both active in v1 |
| SMART attribute depth | Curated attributes as entities by default; `full` opt-in adds a raw SMART JSON diagnostic sensor |
| Component hierarchy | Grouped by default (disks, NVMe, GPU, RAID/pools, docker as sub-devices); `flat` disables |
| Host-to-host hierarchy | Opt-in per VM via a `parent` config key |

## 5. Architecture overview

One Go module, internal packages with single responsibilities.

- `config`: load YAML, apply defaults, interpolate `${ENV}` secrets, layer flags over file over defaults
- `detect`: capability detection, powers `--dump-detected` and `--list-collectors`
- `model`: `Snapshot`, `Metric`, `Device`, and the device tree
- `collector`: the `Collector` interface and one implementation per metric family
- `sink`: the `Sink` interface with `mqtt` and `webhook` implementations
- `ha`: builds Home Assistant discovery payloads for the MQTT sink
- `control`: the command set and its two front-ends (MQTT subscriber, HTTP server)
- `update`: GitHub-Releases self-update
- `main`: flags, lifecycle, scheduler

Data flow. A scheduler runs each available collector on its own cadence. Collectors write into a shared, normalized snapshot. After each fast cycle (and whenever a slow collector refreshes), the current snapshot is handed to every enabled sink, which renders it to its transport. Control commands arrive over MQTT or HTTP, run through one command dispatcher, and publish their result back so it is visible in Home Assistant.

Collector interface (illustrative):

```go
type Collector interface {
    Name() string                       // stable family name, e.g. "smart"
    Available(ctx context.Context) bool // autodetect on this host
    Interval() time.Duration            // own cadence; fast loop or longer
    Collect(ctx context.Context) ([]Metric, error)
}
```

Each collector runs with its own timeout and error isolation. A slow or failing collector never stalls the cycle or crashes the agent; its metrics are simply published as unavailable until it recovers.

## 6. Data model

Snapshot: the host device identity, a timestamp, the agent metadata, and a flat list of metrics that each name the component they belong to.

Metric fields (illustrative):

```go
type Metric struct {
    Key         string      // canonical snake_case key, e.g. "disk_temperature"
    Component   string      // sub-device id; empty means the host device
    Name        string      // short human leaf, e.g. "Temperature"
    Value       any         // number, string, or bool
    Unit        string      // "%", "°C", "d", "MB/s", "h"
    DeviceClass string      // HA device_class, e.g. "temperature", "data_size"
    StateClass  string      // "measurement", "total", "total_increasing"
    Kind        MetricKind  // sensor, binary_sensor, count, text, update
    Category    string      // "primary" or "diagnostic"
    Icon        string      // optional mdi icon
}
```

The canonical key vocabulary is fixed and shared across all hosts so that a key means the same thing everywhere, which keeps dashboards and automations portable. Keys are defined in section 9.

## 7. Device hierarchy, identity, and naming

Two optional hierarchies, both expressed in Home Assistant through `via_device`.

Host-to-host. Each host has a short `node` key (default: sanitized hostname; overridable). A VM sets `parent: <parent-node>`, and its Home Assistant device declares `via_device` equal to the parent host's identifier, so Home Assistant nests the VM under the physical machine. The child declares its parent; the host does not enumerate guests. If the parent agent is offline, Home Assistant shows the link as pending.

Host-to-component. With `hierarchy: grouped` (default), each hardware component becomes its own Home Assistant sub-device with `via_device` equal to the host, so the expanded catalog stays readable instead of piling 40 entities onto one device. Sub-devices are created for each physical disk, each NVMe, each GPU, each RAID array or pool, and docker. With `hierarchy: flat`, all entities attach to the host device.

Identity versus name. This separation is what fixes unreadable serial-based names.

- unique_id and identifiers are hidden, stable, and may be ugly. A disk's unique_id derives from its serial or WWN so it survives the kernel re-lettering the drive (sda becoming sdb). These are never shown
- display names are short, human, and hierarchical through the device tree, not through long strings. Host device name is the node (`gc01srvr`); a sub-device is `Disk sda`, `GPU 0`, `RAID md0`, or a configured alias; entities set `has_entity_name: true` so the entity name is only the short leaf (`Temperature`, `Health`, `Power on hours`) and Home Assistant composes the rest from the device path
- serials, WWNs, and UUIDs appear only as entity values in diagnostic entities, never in a device or entity display name
- `entity_id` is a clean slug from node, component, and metric key (`sensor.gc01srvr_disk_sda_temperature`), readable even though the hidden unique_id carries the serial

Identifier scheme:

- host device identifier: `server-status-<node>`
- sub-device identifier: `server-status-<node>-<component>`, for example `server-status-gc01srvr-disk-<serial-or-wwn>`, `-gpu-<index>`, `-raid-<array>`, `-docker`
- entity unique_id: `<sub-device-identifier>-<metric-key>`

Disk aliasing gives memorable names: `disks: { "WD-WMC4N1234567": "Parity" }` maps a stable serial to a friendly name, and because unique_id is serial-based the alias sticks even if the kernel re-letters the drive.

Worked example. Physical host `gc01srvr` appears as a host device (CPU usage, CPU temperature, load, memory, swap, uptime, root usage, failed units, reboot required, APT updates, agent version as an update entity, last seen), plus sub-devices: `gc01srvr Disk sda` (health, temperature, power-on hours, reallocated sectors, pending sectors, CRC errors, and diagnostic serial, model, firmware, capacity), `gc01srvr NVMe0` (temperature, percentage used, available spare, media errors, unsafe shutdowns, data written, diagnostic serial), `gc01srvr RAID md0` (state, degraded, active devices, resync progress), `gc01srvr GPU 0` (temperature, utilization, memory used, power, fan, diagnostic name and driver), `gc01srvr Docker` (running, unhealthy, updates available, diagnostic containers list). A VM `gc01vm-web` with `parent: gc01srvr` appears as its own device nested under `gc01srvr`.

Home Assistant validation. During implementation the generated discovery payloads will be validated against the live Home Assistant instance through the connected HA integration, so the hierarchy and names are confirmed on real Home Assistant, not only asserted in tests.

## 8. Autoconfiguration

Detection runs at startup and re-evaluates periodically for docker hotplug. A collector publishes only if it has real data.

Always on Debian/Ubuntu: cpu usage, load average, memory used and available, swap, uptime.

Detected when present:

- filesystems: real mounts enumerated from `/proc/self/mountinfo`, pseudo and virtual filesystems filtered out (tmpfs, devtmpfs, overlay, squashfs, snap loops, cgroup, and similar). Replaces the manual `mounts:` map; config can add or exclude specific paths
- SMART: physical disks enumerated from `/sys/block` (loop, ram, dm, md excluded), read via `smartctl` when smartmontools is installed. HDSentinel stays supported as an optional alternative source
- arrays and pools: mdadm arrays auto-discovered from `/proc/mdstat`; ZFS via `zpool` and btrfs when those tools and filesystems exist
- temperatures: read directly from `/sys/class/hwmon` first (no lm-sensors dependency), `sensors` used only as a fallback
- GPU: nvidia via `nvidia-smi`; AMD via `rocm-smi` when present
- network: real interfaces from `/proc/net/dev`, loopback and virtual interfaces skipped by default
- apt: upgradable count, security-only count, reboot-required from `/var/run/reboot-required`
- systemd: failed-unit count and names
- docker: active only when the docker socket exists

`--dump-detected` prints, per host, which collectors fired, the metric values that would publish, and the discovery topics, so a host can be previewed before enabling sinks. `--list-collectors` shows available versus skipped collectors and the reason each was skipped.

## 9. Collector catalog and canonical keys

Everything is autodetected and individually toggleable. Heavy probes (full SMART, dmidecode, docker digests, apt) are cached on their own cadence. The table lists the canonical keys grouped by component. Diagnostic entities carry `entity_category: diagnostic` so they do not clutter the main device view.

Compute and memory (host component):

| Key | Kind | Unit | Notes |
|-----|------|------|-------|
| cpu_usage | sensor | % | overall |
| cpu_core_usage | sensor | % | opt-in, per core |
| cpu_frequency | sensor | MHz | current |
| cpu_temperature | sensor | °C | package |
| load_1m, load_5m, load_15m | sensor | | load average |
| memory_used, memory_available | sensor | % | |
| swap_used | sensor | % | |
| uptime | sensor | d | 2 decimals under 10 days |

System and OS (host, mostly diagnostic): cpu_model, cpu_cores, cpu_threads, cpu_governor, board_vendor, board_product, bios_version, chassis_type, system_serial (diagnostic), os_name, os_version, kernel, architecture, virtualization, boot_time. Optional DIMM inventory via dmidecode (dimm_size, dimm_speed, dimm_part) behind a config flag.

Storage, per physical disk sub-device:

| Key | Kind | Unit | Notes |
|-----|------|------|-------|
| disk_health | sensor | | SMART overall PASSED/FAILED, or HDSentinel percent |
| disk_temperature | sensor | °C | |
| disk_power_on_hours | sensor | h | total_increasing |
| disk_power_cycles | sensor | | total_increasing |
| disk_reallocated_sectors | sensor | | |
| disk_pending_sectors | sensor | | |
| disk_offline_uncorrectable | sensor | | |
| disk_crc_errors | sensor | | UDMA CRC |
| disk_percentage_used | sensor | % | SSD/NVMe wear |
| disk_available_spare | sensor | % | NVMe |
| disk_media_errors | sensor | | NVMe |
| disk_unsafe_shutdowns | sensor | | NVMe |
| disk_data_written | sensor | TB | total_increasing |
| disk_model, disk_serial, disk_firmware, disk_capacity, disk_rotation, disk_interface | text | | diagnostic |

With `smart_attributes: full`, an additional `disk_smart_raw` diagnostic text sensor carries the full smartctl JSON.

Filesystems, per mount (host device by default): fs_usage, fs_used_bytes, fs_total_bytes, fs_inode_usage, fs_type (diagnostic), fs_read_only (binary_sensor, problem). Note the deliberate split from the physical `disk_*` keys above: a mounted filesystem and a physical drive are different things, so `fs_usage` (capacity of a mount) never collides with `disk_temperature` (hardware of a drive).

Arrays and pools, per array or pool sub-device: raid_state (text), raid_degraded (binary_sensor, problem), raid_active_devices, raid_failed_devices, raid_spare_devices, raid_resync_progress (%), raid_resync_speed; for ZFS pool_health, pool_capacity, pool_fragmentation, pool_read_errors, pool_write_errors, pool_checksum_errors, pool_scrub_state; for btrfs allocation and device error stats.

Thermal and power (host): hwmon temperatures beyond CPU (chipset, nvme, drivetemp) as temperature sensors, fan_speed (RPM) per fan, optional voltages. Optional UPS via NUT (ups_battery %, ups_load %, ups_runtime, ups_status text) when `upsc` is present.

GPU, per GPU sub-device: gpu_temperature, gpu_utilization, gpu_memory_used, gpu_power, gpu_power_limit, gpu_fan, gpu_clock, and diagnostic gpu_name and gpu_driver.

Network, per interface (host or optional per-interface): net_rx_rate, net_tx_rate (MB/s), net_link_speed, net_operstate (binary_sensor), net_rx_errors, net_tx_errors, net_rx_drops, net_tx_drops, and diagnostic net_mac and net_ip.

Software and services (host): apt_updates (count), apt_security_updates (count), reboot_required (binary_sensor, update), apt_upgradable_list (diagnostic text), systemd_failed_units (count), systemd_failed_list (diagnostic text), optional per-service up/down from a config watch-list, optional time_sync (binary_sensor), process_count, zombie_count, and diagnostic top_cpu_process and top_memory_process.

Docker (docker sub-device): docker_version (diagnostic), docker_running (count), docker_stopped (count), docker_unhealthy (count), docker_restarting (count), docker_updates_available (count), docker_containers (diagnostic text: per-container name, compose project, state, image, update-available), optional per-container binary_sensors behind a config flag, optional docker_disk_usage.

Agent self (host, diagnostic except the update entity): agent_version and agent_latest surfaced as a Home Assistant update entity, last_seen (timestamp), cycle_duration, detected_collectors, config_hash.

## 10. Transports and parity

Sinks are a configurable list; enable any combination, and every enabled sink receives the identical snapshot.

MQTT sink. Connects to the broker, publishes retained Home Assistant discovery, state topics, and an availability LWT. Default base topic `server-status/<node>`. Ports the current Python reconnect behavior, which is sound: a reconnect worker with capped backoff, retained discovery, wait-for-connection before publishing, and cached-state replay after reconnect. Discovery builds one device block per host and per sub-device, wiring `via_device` up the tree and setting `has_entity_name: true`.

Webhook sink. POSTs the snapshot JSON to one or more URLs each cycle, or on change, with an auth header or token and retry with backoff. The payload is the normalized snapshot: a device block (id, node, name, parent, model), a timestamp, and a metrics array where each item carries key, component, name, value, unit, device_class, kind, and category. This is exactly what n8n consumes.

Parity is structural. A metric added to any collector appears in the MQTT sink, the webhook sink, and the HTTP snapshot with no extra work. A golden test asserts the two sinks emit an identical metric set.

## 11. Control surface

Both channels active in v1, running through one command dispatcher. Commands: `update` (self-update to latest), `refresh` (immediate collect and publish), `rescan` (re-run detection), `restart`, and `rollback`.

MQTT control. Subscribes to `server-status/<node>/cmd/<command>`, exposes Home Assistant button entities plus the agent update entity, and publishes each command's result to `server-status/<node>/cmd/<command>/result`, mirrored into a diagnostic last-command-result sensor.

HTTP control. A small server with a configurable bind address, port, and bearer token, exposing `POST /command/{name}` (token required), `GET /health`, and `GET /snapshot` (returns the current snapshot for n8n pull or debugging). Binds to LAN or localhost by default.

## 12. Self-update

Release pipeline. A GitHub Actions workflow builds static linux/amd64 and linux/arm64 binaries on each `v*` tag and publishes a release with SHA256 checksums. Checksum verification is the default integrity check; minisign or cosign signatures are a later hardening option.

Update flow. Triggered by the Home Assistant update entity, an MQTT command, or HTTP. The agent queries the Releases API for the latest version (also polled periodically to populate the update entity), and if newer downloads the matching-arch asset, verifies its SHA256, writes it as `<binary>.new`, keeps the current binary as `<binary>.bak`, atomically renames the new binary into place, then restarts via `systemctl restart server-status` so systemd relaunches into the new build. The swap happens only after verification, so a bad download leaves the running binary untouched. Every step reports success or failure with versions to the command-result sensor. A `rollback` command swaps `.bak` back. No Go toolchain is ever required on a host. The binary is version-stamped at build time via ldflags.

## 13. Deployment and packaging

An idempotent `install.sh` detects arch, downloads the latest release binary to `/opt/server-status/`, writes a default `/etc/server-status/config.yaml` when none exists, installs the systemd unit, and enables the service; re-running upgrades. A `curl … | sh` one-liner wraps it. On first run with no sink configured, the agent runs in detect-and-print mode and refuses to start sinks until a minimal config exists.

The systemd unit uses `Type=notify` with `WatchdogSec` for self-healing and `Restart=always`. The agent runs as root because smartctl, mdadm, writing its own binary, and calling systemctl require privilege; systemd sandboxing (ProtectHome, ReadWritePaths limited to the binary and state directories) is applied where it does not break hardware access. State and caches live in `/var/lib/server-status/`, replacing `/var/tmp`. `install.sh --uninstall` stops and removes the service; `--purge` also clears retained discovery so the Home Assistant device disappears cleanly.

## 14. Reliability

Beyond the ported MQTT reconnect and retained-discovery behavior: the systemd watchdog restarts a hung agent; per-collector timeouts and error isolation keep one slow probe from stalling the cycle; slow collectors run cached on their own cadence off the fast loop; shutdown publishes offline and flushes; logging is structured with an optional debug level. The last_seen sensor plus the availability LWT let Home Assistant notice a dead agent, where the age threshold and notification are configured.

## 15. Security considerations

Control commands, especially update and restart, are privileged. Guardrails: MQTT ACLs should restrict the `cmd` topics to trusted publishers; the HTTP endpoint requires a bearer token and binds to LAN or localhost by default; self-update downloads only from the configured GitHub repository over HTTPS and verifies the checksum before swapping; commands are rate-limited; each control channel and each command can be disabled in config. The spec documents that anyone able to publish to the `cmd` topic or reach the HTTP endpoint with the token can trigger an update or restart. Secrets (broker password, webhook token, HTTP token) support `${ENV}` interpolation so they need not sit in plaintext in the config file.

## 16. Configuration

Layering: built-in defaults, then `/etc/server-status/config.yaml`, then `${ENV}` interpolation for secrets, then flags.

Minimum viable config:

```yaml
sinks:
  - type: mqtt
    host: 192.168.1.65
    username: mqtt
    password: ${MQTT_PASSWORD}
```

Everything else autodetects. Annotated fuller example:

```yaml
node: gc01srvr            # short host key; default sanitized hostname
friendly_name: gc01srvr   # host device display name; default node
parent:                   # parent node key for host-to-host nesting; VMs set this
hierarchy: grouped        # grouped (sub-devices) or flat

sinks:
  - type: mqtt
    host: 192.168.1.65
    port: 1883
    username: mqtt
    password: ${MQTT_PASSWORD}
    base_topic: server-status
    discovery_prefix: homeassistant
    retain: true
    qos: 0
    tls: false
  - type: webhook
    url: https://n8n.example/webhook/server-status
    token: ${WEBHOOK_TOKEN}
    on_change: false

control:
  mqtt: true
  http:
    enabled: true
    bind: 0.0.0.0
    port: 9971
    token: ${HTTP_TOKEN}

update:
  repo: giovi321/server-status
  channel: stable
  check_interval_seconds: 21600

collectors:            # all default on when detected; override here
  gpu: true
  docker: true
  smart_attributes: curated   # curated or full
  per_container_entities: false

disks:                 # optional serial/WWN aliases and include/exclude
  "WD-WMC4N1234567": "Parity"

services:              # optional systemd watch-list, each exposed up/down
  - nginx
  - postgresql

intervals:             # optional per-collector cadence overrides (seconds)
  smart: 1800
  docker: 21600
  apt: 3600
```

## 17. Testing strategy

Unit tests with fixture files for every parser (proc/stat, meminfo, mountinfo, mdstat, smartctl JSON, hwmon, docker digest compare, nvidia-smi), which the current tool entirely lacks. Golden tests assert the Home Assistant discovery payloads and, critically, that the MQTT and webhook sinks emit an identical metric set, enforcing parity. A `--once --dry-run` smoke run exercises the whole pipeline on a real host. CI builds, tests, and lints (golangci-lint) on push and cuts a release on tag. Discovery payloads are additionally validated against the live Home Assistant instance during implementation.

## 18. Migration

The new default base topic (`server-status/<node>`) means Home Assistant creates fresh, correctly separated devices, and the old `SERVER` device is deleted once. A config option can pin a host to the old `SERVER` base topic and entity ids if preserving entity history for that host matters, but the clean re-register is recommended given the per-host separation requirement and the current identifier-collision bug. The Python tool stays runnable during the transition, and hosts can be cut over one at a time.

## 19. Rough phasing

This is guidance for the implementation plan, not a commitment.

1. Skeleton: module layout, config, model, scheduler, `--dump-detected`, one core collector (cpu/mem/uptime), MQTT sink with per-host device and discovery, systemd unit, install script
2. Autoconfiguration and the core collector set (load, swap, filesystems, network, temperatures, apt, systemd failed units, reboot required)
3. Device hierarchy and naming: sub-devices, `via_device`, `has_entity_name`, disk aliasing, host-to-host parent
4. Rich storage: SMART curated and full, mdadm, ZFS, btrfs; GPU
5. Docker: registry digest compare, container inventory, compose awareness
6. Webhook sink and the HTTP control surface, parity golden tests
7. Control commands and self-update, GitHub Releases pipeline, update entity
8. Reliability hardening: watchdog, per-collector isolation, cached slow collectors; uninstall and purge
9. Home Assistant validation against the live instance; migration cutover

## 20. Future, out of scope for v1

- Host-side auto-enumeration of guest VMs via libvirt or Proxmox
- minisign or cosign signing of release binaries
- AMD GPU parity with nvidia
- Certificate-expiry and fail2ban collectors
- Non-Debian Linux and non-Linux hosts
