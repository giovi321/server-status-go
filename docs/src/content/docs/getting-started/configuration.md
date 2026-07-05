---
title: "Configuration"
description: "A walk-through of the config file, from a minimal MQTT setup to sinks, control, and hierarchy"
---

server-status reads one YAML config file, given with `-c` (or `--config`). This page walks through the common cases. For the exhaustive list of every field and default, see the [Configuration reference](../../reference/config/).

## The smallest useful config

```yaml
node:            # blank -> defaults to the hostname
sinks:
  - type: mqtt
    host: 192.168.1.65
    username: mqtt
    password: ${MQTT_PASSWORD}
```

That publishes every auto-detected metric to the broker at `192.168.1.65:1883` under the base topic `server-status`, with Home Assistant discovery under the `homeassistant` prefix. The host appears in Home Assistant as a device named after its node.

## Secrets with `${ENV}`

Any `${VAR}` in the config is replaced with that environment variable's value before parsing. Keep the broker password out of the file:

```yaml
password: ${MQTT_PASSWORD}
```

Under systemd, the value comes from the environment file the unit loads (`/etc/server-status/server-status.env`). A bare `$` that is not part of `${...}` is left untouched.

## Naming the device

```yaml
node: gc01srvr           # slug used in topics and entity ids
friendly_name: GC01 srvr # human label shown in Home Assistant
```

`node` is sanitized to `[a-z0-9-]` and used in MQTT topics and entity ids. `friendly_name` is the display name; if omitted it falls back to the node. See [Entity naming](../../home-assistant/naming/).

## Two sinks at once

MQTT and webhook can run together. Both receive the same snapshot, so a second consumer (for example n8n) sees identical data. See [Webhook sink](../../control/webhook/).

```yaml
sinks:
  - type: mqtt
    host: 192.168.1.65
    username: mqtt
    password: ${MQTT_PASSWORD}
  - type: webhook
    url: https://n8n.example.com/webhook/server-status
    token: ${WEBHOOK_TOKEN}   # sent as Authorization: Bearer
    on_change: true           # POST only when metrics change
```

## Control surface

Enable the read-only HTTP endpoint and command dispatch. Commands (refresh, restart, update) require a token; set one to use them. See [HTTP control API](../../control/http/).

```yaml
control:
  http:
    enabled: true
    bind: 127.0.0.1
    port: 9099
    token: ${CONTROL_TOKEN}
```

## Self-update source

The self-update flow pulls from a GitHub repository's latest release. The default already points at the upstream repo, so you only set this to track a fork.

```yaml
update:
  repo: giovi321/server-status-go
  check_interval_seconds: 21600
```

## Device hierarchy

Nest a host under a parent host (for example a VM under its hypervisor), and control whether disks/GPUs/arrays appear as sub-devices. See [Device hierarchy](../../home-assistant/hierarchy/).

```yaml
parent: gc01srvr     # this host's device links via_device to the parent
hierarchy: grouped   # "grouped" (sub-devices) or "flat" (everything on the host device)
```

## Disk names and SMART detail

Give disks friendly aliases (instead of raw serials in entity names) and choose how many SMART attributes to expose:

```yaml
disks:
  "WD-WCC4N7XXXXXX": data-1     # serial -> alias
smart_attributes: curated       # "curated" (default) or "full"
```

`full` adds a `disk_smart_raw` diagnostic (the whole parsed smartctl output as JSON) on top of the curated set.

## Applying changes

Restart the service after editing:

```bash
sudo systemctl restart server-status
```

To preview the effect without a broker, run `server-status -c config.yaml --dump-detected`. See [Running the agent](../running/).
