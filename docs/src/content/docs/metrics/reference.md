---
title: "Metrics reference"
description: "Every metric key, its unit, entity type, and category"
---

This is the complete catalog: 16 collectors, 68 distinct metric keys. Column meanings:

- **kind** is the entity type. `sensor` and `binary_sensor` map to those Home Assistant domains. `text` is published as a plain `sensor` (there is no separate text domain in the discovery)
- **category** `primary` entities show as normal controls; `diagnostic` entities are grouped under the device's diagnostic section in Home Assistant
- **instance** marks multi-instance metrics (one per mount, interface, or sensor)

Values are published as strings on MQTT; binary sensors use `ON`/`OFF`.

## Core

| key | name | unit | kind | category |
|-----|------|------|------|----------|
| `cpu_usage` | CPU usage | % | sensor | primary |
| `memory_used` | Memory used | % | sensor | primary |
| `memory_available` | Memory available | % | sensor | primary |
| `swap_used` | Swap used | % | sensor | primary |
| `uptime` | Uptime | d | sensor | primary |
| `load_1m` | Load 1m | | sensor | primary |
| `load_5m` | Load 5m | | sensor | primary |
| `load_15m` | Load 15m | | sensor | primary |

## Filesystem (per mount)

One set per real mount. Instance is the mount path (`/` slugifies to `root`).

| key | name | unit | kind | category |
|-----|------|------|------|----------|
| `fs_usage` | `<mount> usage` | % | sensor | primary |
| `fs_used_bytes` | `<mount> used` | B | sensor | diagnostic |
| `fs_total_bytes` | `<mount> size` | B | sensor | diagnostic |
| `fs_inode_usage` | `<mount> inode usage` | % | sensor | diagnostic |
| `fs_type` | `<mount> filesystem` | | text | diagnostic |
| `fs_read_only` | `<mount> read only` | | binary_sensor | diagnostic |

## Network (per interface)

Loopback and virtual interfaces (`docker`, `veth`, `br-`, `virbr`, `tun`, `tap`, and similar) are excluded. Rates need two samples, so nothing is emitted on the first cycle.

| key | name | unit | kind | category |
|-----|------|------|------|----------|
| `net_rx_rate` | `<iface> rx rate` | MB/s | sensor | primary |
| `net_tx_rate` | `<iface> tx rate` | MB/s | sensor | primary |
| `net_operstate` | `<iface> link` | | binary_sensor | diagnostic |

## Temperature (per sensor)

Read from hwmon. Instance is `<chip>-<sensor>` so identically named chips do not collide.

| key | name | unit | kind | category |
|-----|------|------|------|----------|
| `temperature` | `<label> temperature` | °C | sensor | primary |

## Software

| key | name | unit | kind | category |
|-----|------|------|------|----------|
| `apt_updates` | APT updates | | sensor | primary |
| `apt_security_updates` | APT security updates | | sensor | primary |
| `reboot_required` | Reboot required | | binary_sensor | primary |
| `systemd_failed_units` | Failed units | | sensor | primary |
| `systemd_failed_list` | Failed units list | | text | diagnostic |

## SMART (per disk sub-device)

Each physical disk is a sub-device. A metric appears only when the value is present in the `smartctl` output, so ATA and NVMe disks expose different subsets.

| key | name | unit | kind | category | notes |
|-----|------|------|------|----------|-------|
| `disk_health` | Health | | binary_sensor | primary | `problem` when SMART status is not "passed" |
| `disk_temperature` | Temperature | °C | sensor | primary | |
| `disk_power_on_hours` | Power on hours | h | sensor | primary | |
| `disk_power_cycles` | Power cycles | | sensor | primary | |
| `disk_reallocated_sectors` | Reallocated sectors | | sensor | primary | ATA |
| `disk_pending_sectors` | Pending sectors | | sensor | primary | ATA |
| `disk_offline_uncorrectable` | Offline uncorrectable | | sensor | primary | ATA |
| `disk_crc_errors` | CRC errors | | sensor | primary | ATA |
| `disk_percentage_used` | Percentage used | % | sensor | primary | NVMe |
| `disk_available_spare` | Available spare | % | sensor | primary | NVMe |
| `disk_media_errors` | Media errors | | sensor | primary | NVMe |
| `disk_unsafe_shutdowns` | Unsafe shutdowns | | sensor | primary | NVMe |
| `disk_data_written` | Data written | B | sensor | primary | NVMe |
| `disk_model` | Model | | text | diagnostic | |
| `disk_serial` | Serial | | text | diagnostic | |
| `disk_firmware` | Firmware | | text | diagnostic | |
| `disk_capacity` | Capacity | B | sensor | diagnostic | |
| `disk_rotation` | Rotation | | text | diagnostic | `SSD` or `<N> rpm` |
| `disk_smart_raw` | SMART raw | | text | diagnostic | only with `smart_attributes: full` |

## mdadm (per array sub-device)

| key | name | unit | kind | category | notes |
|-----|------|------|------|----------|-------|
| `raid_state` | State | | text | primary | |
| `raid_degraded` | Degraded | | binary_sensor | primary | `problem` when failed or not active/clean |
| `raid_active_devices` | Active devices | | sensor | primary | |
| `raid_total_devices` | Total devices | | sensor | diagnostic | |
| `raid_failed_devices` | Failed devices | | sensor | primary | |
| `raid_resync_progress` | Resync progress | % | sensor | primary | only while resync/recovery is active |

## GPU (per GPU sub-device, NVIDIA)

| key | name | unit | kind | category | notes |
|-----|------|------|------|----------|-------|
| `gpu_temperature` | Temperature | °C | sensor | primary | |
| `gpu_utilization` | Utilization | % | sensor | primary | |
| `gpu_memory_used` | Memory used | % | sensor | primary | |
| `gpu_power` | Power | W | sensor | primary | |
| `gpu_power_limit` | Power limit | W | sensor | diagnostic | |
| `gpu_fan` | Fan | % | sensor | primary | if reported |
| `gpu_name` | Name | | text | diagnostic | |
| `gpu_driver` | Driver | | text | diagnostic | |

## ZFS (per pool sub-device)

| key | name | unit | kind | category | notes |
|-----|------|------|------|----------|-------|
| `pool_health` | Health | | text | primary | |
| `pool_degraded` | Degraded | | binary_sensor | primary | `problem` when not ONLINE |
| `pool_capacity` | Capacity | % | sensor | primary | |
| `pool_fragmentation` | Fragmentation | % | sensor | diagnostic | when reported |

## Docker (one Docker sub-device)

| key | name | unit | kind | category | notes |
|-----|------|------|------|----------|-------|
| `docker_running` | Running | | sensor | primary | |
| `docker_stopped` | Stopped | | sensor | primary | exited + created + dead |
| `docker_restarting` | Restarting | | sensor | primary | |
| `docker_unhealthy` | Unhealthy | | sensor | primary | |
| `docker_updates_available` | Updates available | | sensor | primary | distinct images with a newer registry digest |
| `docker_containers` | Containers | | text | diagnostic | JSON inventory of every container |

## Agent

| key | name | unit | kind | category | notes |
|-----|------|------|------|----------|-------|
| `last_seen` | Last seen | | sensor | diagnostic | RFC3339 timestamp, refreshed every cycle |
| `agent_version` | Agent version | | text | diagnostic | the running build version |

Use `last_seen` as a liveness signal in Home Assistant: an automation can alert if the timestamp goes stale.
