---
title: "Entity naming"
description: "How node, friendly name, and disk aliases shape entity ids and labels"
---

Names are built to be short, consistent, and stable. Three inputs shape them: the node, the friendly name, and disk aliases.

## Node vs friendly name

- **`node`** is the stable identity. It is sanitized to `[a-z0-9-]` and used in every MQTT topic, `object_id`, and `unique_id`. Changing it renames all the topics, so pick it once
- **`friendly_name`** is only the display name of the device in Home Assistant. Change it freely without breaking anything

```yaml
node: gc01srvr           # -> topics and ids
friendly_name: GC01 srvr # -> device label in HA
```

If `friendly_name` is omitted it falls back to the node.

## How labels are built

Entities set `has_entity_name`, so Home Assistant renders them as "device name + entity name" and keeps the entity list under the device short. For the host device "GC01 srvr":

- `cpu_usage` shows as **CPU usage**
- `fs_usage` on `/data` shows as **/data usage**
- On the disk sub-device, `disk_temperature` shows as **Temperature**

## Ids

For node `gc01srvr`:

| Metric | object_id | unique_id |
|--------|-----------|-----------|
| `cpu_usage` (host) | `gc01srvr_cpu_usage` | `server-status-gc01srvr-cpu_usage` |
| `fs_usage` on `/` | `gc01srvr_fs_usage_root` | `server-status-gc01srvr-fs_usage-root` |
| `disk_temperature` on disk sub-device | `gc01srvr_disk-<slug>_disk_temperature` | `server-status-gc01srvr-disk-<slug>-disk_temperature` |

The `object_id` seeds the Home Assistant `entity_id`; the `unique_id` is the hidden, stable registry id.

## Keep disk names short

Disks would otherwise be identified by serial number, which is long and unfriendly. Map each serial to a short alias so the sub-device reads well:

```yaml
disks:
  "WD-WCC4N7XXXXXX": data-1
  "S3Z1NB0KXXXXXX": ssd-boot
```

With an alias, the sub-device is named "GC01 srvr Disk data-1". Without one, it falls back to a name based on the device node (for example "Disk sda"). The serial still lives in the hidden `unique_id`, so aliases can change without orphaning entities. Find a disk's serial with `smartctl -i /dev/sdX`, or from the `disk_serial` diagnostic entity.

## Sub-device names

Sub-device labels follow "host name + component":

- "GC01 srvr Disk data-1"
- "GC01 srvr RAID md0"
- "GC01 srvr GPU 0"
- "GC01 srvr Pool tank"
- "GC01 srvr Docker"

## Recommendations

- Set a readable `friendly_name` on every host
- Give every disk you care about an alias in `disks:`
- Choose `node` values that sort and read well together (for example `gc01srvr`, `gc02srvr`), since they are what you will see in topics and any raw MQTT tooling
