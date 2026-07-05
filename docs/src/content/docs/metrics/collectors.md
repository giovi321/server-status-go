---
title: "Collectors"
description: "The 16 collectors, when each is available, and what it reports"
---

A collector is a pure reader of one data source (`/proc`, `/sys`, or a standard CLI). On each cycle the agent runs every **available** collector and merges their metrics into one snapshot. A collector is available only when the host actually has the thing it measures, which is what keeps a VPS from sprouting temperature sensors and a diskless host from showing SMART entities.

## How collection works

- **Autodetection**. `Available()` decides per host. `server-status --dump-detected` shows exactly which collectors are available and what they would publish
- **Serial with a timeout**. Collectors run one after another in a fixed order. Each `Collect` is bounded by a 20 second timeout, and a panicking collector is isolated (it yields no metrics that cycle instead of crashing the agent). See [systemd and watchdog](../../reliability/systemd/)
- **Some collectors are stateful**. Network rates need two samples, so the network collector emits nothing on its first cycle

## The collectors

| Collector | Available when | Publishes | Sub-device |
|-----------|----------------|-----------|------------|
| `cpu` | `/proc/stat` readable | CPU usage % (samples twice, 1s apart) | host |
| `memory` | `/proc/meminfo` has `MemTotal` | Used and available % | host |
| `swap` | swap is configured (`SwapTotal > 0`) | Swap used % | host |
| `uptime` | `/proc/uptime` readable | Uptime in days | host |
| `load` | `/proc/loadavg` readable | Load 1m/5m/15m | host |
| `filesystem` | at least one real mount | Per-mount usage, size, inodes, type, read-only | host, per mount |
| `network` | at least one real interface | Per-interface rx/tx rate and link state | host, per interface |
| `temperature` | a hwmon `tempN_input` exists | Per-sensor temperature | host, per sensor |
| `apt` | `apt-get` present | Updates, security updates, reboot-required | host |
| `systemd` | `systemctl` present | Failed unit count and list | host |
| `smart` | `smartctl` present and ≥1 physical disk | Per-disk health, temperature, wear, identity | one per disk |
| `mdadm` | `/proc/mdstat` has an array | Per-array state, device counts, resync | one per array |
| `gpu` | `nvidia-smi` present | Per-GPU temperature, utilization, memory, power, fan | one per GPU |
| `zfs` | `zpool` present | Per-pool health, capacity, fragmentation | one per pool |
| `docker` | `docker` CLI present | Container counts, unhealthy, updates available, inventory | one `Docker` sub-device |
| `agent` | always | Last-seen heartbeat and agent version | host |

See the [Metrics reference](../reference/) for every key, unit, and entity type.

## Notes on detection

- **`smart`** caches results for 30 minutes so drives are not woken every cycle. Disk aliases come from the `disks:` config; without an alias a disk uses a fallback name. `smart_attributes: full` adds a raw diagnostic
- **`docker`** counts `docker ps -a` every cycle, but the registry update scan (which images have a newer digest) is cached for 6 hours. It never pulls images; it compares digests using `skopeo` if present, otherwise `docker manifest inspect`. A locally-built image with no registry digest counts as "no update"
- **`zfs` and `docker` availability track the tool, not the workload**. A host with `zpool` installed but no pools, or the `docker` CLI installed but the daemon stopped, reports the collector as available and simply publishes no items. This is by design; it avoids flapping when a pool or the daemon comes and goes
- **`temperature`, `network`, `filesystem`** are multi-instance: they emit one set of metrics per sensor, interface, or mount, distinguished by an instance label

## GPU support

Only NVIDIA GPUs are read today, via `nvidia-smi`. AMD GPU support is a possible future addition.

## MQTT is plaintext

The MQTT sink connects over plain TCP; there is no TLS/`mqtts` support. Run the broker on a trusted network or reach it over a VPN.
