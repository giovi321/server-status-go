---
title: "Metrics reference"
description: "Every metric key, its unit, entity type, and category"
---

This is the complete catalog: 17 collectors, 86 distinct metric keys. Column meanings:

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

## Rsnapshot (per config sub-device)

Each monitored rsnapshot config file is a sub-device (`main` for `/etc/rsnapshot.conf`, otherwise the filename minus the `rsnapshot-` prefix and `.conf` suffix). All values come from file reads; the collector never runs rsnapshot.

The schedule that drives each interval, and the staleness bound derived from it, are read from the systemd timers that start `rsnapshot@<interval>.service`, falling back to the crontab on hosts still triggered that way. On a timer-driven host `rsnapshot_cron_jobs` is `0` and `rsnapshot_timer_jobs` carries the coverage; on a cron host it is the reverse.

`rsnapshot_state` is an enum: `error`, `stuck`, `stale`, `running`, `pending`, `warning`, `unknown`, `ok`. `pending` means the first snapshot has not appeared yet and nothing else is wrong. `rsnapshot_last_result` is `success`, `warnings`, `errors`, `running`, `died`, or `unknown`.

`rsnapshot_interval_age` is multi-instance: one sensor per retain interval, with the interval name (`hoursago`, `daily`, ...) as the instance.

| key | name | unit | kind | category | notes |
|-----|------|------|------|----------|-------|
| `rsnapshot_problem` | Problem | | binary_sensor | primary | `problem` when state is `error`, `stale`, or `stuck` |
| `rsnapshot_state` | State | | text | primary | see the enum above |
| `rsnapshot_last_result` | Last result | | text | primary | outcome of the last run seen in the log |
| `rsnapshot_last_success` | Last success | | sensor | primary | `timestamp` device class, RFC3339; omitted until a first success exists |
| `rsnapshot_stale` | Stale | | binary_sensor | primary | `problem`; only emitted when a staleness bound is known (`max_age`, else derived from the systemd timer or cron schedule) |
| `rsnapshot_stuck` | Stuck | | binary_sensor | primary | `problem` when a live run holds the lock longer than `stuck_after` |
| `rsnapshot_interval_age` | `<interval> age` | h | sensor | primary | one per retain interval |
| `rsnapshot_running` | Running | | binary_sensor | diagnostic | `running` device class; a live rsnapshot process holds the lock |
| `rsnapshot_stale_lock` | Stale lock | | binary_sensor | diagnostic | `problem`; lockfile exists but the pid is dead or the content unparseable |
| `rsnapshot_root_missing` | Root missing | | binary_sensor | diagnostic | `problem`; snapshot root is not a readable directory |
| `rsnapshot_root_readonly` | Root read-only | | binary_sensor | diagnostic | `problem`; snapshot root filesystem is mounted read-only |
| `rsnapshot_config_error` | Config error | | binary_sensor | diagnostic | `problem`; conf unreadable, no `snapshot_root`, or no retain intervals |
| `rsnapshot_cron_jobs` | Cron jobs | | sensor | diagnostic | cron entries matched to this config across all intervals (`0` on a timer-driven host) |
| `rsnapshot_cron_list` | Cron list | | text | diagnostic | matched `<interval> <spec>` pairs |
| `rsnapshot_timer_jobs` | Timer jobs | | sensor | diagnostic | enabled systemd timers matched to this config across all intervals |
| `rsnapshot_timer_list` | Timer list | | text | diagnostic | matched `<interval> <OnCalendar>` pairs |
| `rsnapshot_intervals` | Intervals | | text | diagnostic | like `hoursago:6 daysago:7 weeksago:4 monthsago:4` |
| `rsnapshot_stray_items` | Stray items | | sensor | diagnostic | leftover `_delete.*` and `*.sync` entries in the snapshot root |
| `rsnapshot_details` | Details | | text | diagnostic | like `mount:ok rw:ok conf:ok cron:0 timer:4 lock:idle stray:0` |

One host-level metric counts the monitored configs:

| key | name | unit | kind | category | notes |
|-----|------|------|------|----------|-------|
| `rsnapshot_configs` | Rsnapshot configs | | sensor | diagnostic | number of monitored config files |

## Agent

| key | name | unit | kind | category | notes |
|-----|------|------|------|----------|-------|
| `last_seen` | Last seen | | sensor | diagnostic | RFC3339 timestamp, refreshed every cycle |
| `agent_version` | Agent version | | text | diagnostic | the running build version |

Use `last_seen` as a liveness signal in Home Assistant: an automation can alert if the timestamp goes stale.
