---
title: "Configuration reference"
description: "Every configuration field, its type, and its default"
---

The config is a single YAML file passed with `-c`. Any `${VAR}` is expanded from the environment before parsing. This page lists every field. For a guided setup see [Configuration](../../getting-started/configuration/).

## Top level

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `node` | string | hostname | Stable identity used in MQTT topics and entity ids. Sanitized to `[a-z0-9-]` |
| `friendly_name` | string | `node` | Display name of the device in Home Assistant |
| `parent` | string | (none) | Node of a parent host. This device links to it via `via_device`. Sanitized |
| `hierarchy` | string | `grouped` | `grouped` puts disks/GPU/arrays/docker on their own sub-devices; `flat` puts everything on the host device |
| `sinks` | list | (none) | One or more output sinks (see below). At least one is required |
| `disks` | map | (none) | Maps a disk serial (as reported by `smartctl`) to a short alias used in the sub-device name |
| `smart_attributes` | string | `curated` | `curated` publishes a hand-picked set of SMART attributes; `full` additionally emits a `disk_smart_raw` diagnostic containing the whole parsed smartctl output as JSON |
| `control` | object | (none) | The inbound control surface (see below) |
| `update` | object | defaults below | Self-update source (see below) |
| `rsnapshot` | object | defaults below | Rsnapshot backup monitoring (see below) |

## Sinks

Each entry in `sinks` has a `type` of `mqtt` or `webhook`. Fields apply to the type shown.

| Field | Type | Default | Applies to | Description |
|-------|------|---------|-----------|-------------|
| `type` | string | (required) | both | `mqtt` or `webhook` |
| `host` | string | (required) | mqtt | Broker hostname or IP |
| `port` | int | `1883` | mqtt | Broker port |
| `username` | string | (none) | mqtt | Broker username (omit for anonymous) |
| `password` | string | (none) | mqtt | Broker password (use `${MQTT_PASSWORD}`) |
| `base_topic` | string | `server-status` | mqtt | Root of the state/availability/command topics |
| `discovery_prefix` | string | `homeassistant` | mqtt | Home Assistant discovery prefix |
| `retain` | bool | `false` | mqtt | Retain state values. Discovery configs are always retained regardless |
| `qos` | int | `0` | mqtt | MQTT QoS for published messages |
| `url` | string | (required) | webhook | Endpoint the snapshot JSON is POSTed to |
| `token` | string | (none) | webhook | Sent as `Authorization: Bearer <token>` |
| `on_change` | bool | `false` | webhook | POST only when the metrics changed since the last successful delivery |

## Control

```yaml
control:
  http:
    enabled: true
    bind: 127.0.0.1
    port: 9099
    token: ${CONTROL_TOKEN}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `control.http.enabled` | bool | `false` | Start the HTTP control server |
| `control.http.bind` | string | (none) | Bind address. Use `127.0.0.1` unless a remote consumer needs it |
| `control.http.port` | int | (none) | TCP port to listen on |
| `control.http.token` | string | (none) | Bearer token. `/snapshot` is open when unset; commands are refused unless a token is set |

See [HTTP control API](../../control/http/) for endpoint behavior.

## Update

```yaml
update:
  repo: giovi321/server-status-go
  check_interval_seconds: 21600
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `update.repo` | string | `giovi321/server-status-go` | GitHub `owner/name` the self-update pulls its latest release from |
| `update.check_interval_seconds` | int | `21600` | Configured interval for update checks (6 hours) |

## Rsnapshot

```yaml
rsnapshot:
  stuck_after: 12h
  margin: 8h
  configs:
    - path: /etc/rsnapshot-gc01srvr.conf
      name: gc01srvr
      max_age:
        daily: 26h
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `rsnapshot.configs` | list | (none) | Config files to monitor. When empty, `/etc/rsnapshot.conf` and `/etc/rsnapshot-*.conf` are auto-discovered (packaging and editor leftovers like `.dpkg-dist` or `.bak` are skipped) |
| `rsnapshot.configs[].path` | string | (required) | Path of the rsnapshot config file |
| `rsnapshot.configs[].name` | string | derived | Sub-device name. Defaults to `main` for `/etc/rsnapshot.conf`, otherwise the filename minus the `rsnapshot-` prefix and `.conf` suffix |
| `rsnapshot.configs[].max_age` | map | (none) | Per-interval staleness bound, keyed by retain interval name. Overrides the cron-derived bound for that interval |
| `rsnapshot.stuck_after` | string | `12h` | A run whose lock has been held longer than this counts as stuck |
| `rsnapshot.margin` | string | `8h` | Slack added to the cron-derived staleness bound, covering run duration and serialization delays |

Durations use Go syntax (`12h`, `90m`). An empty, invalid, or non-positive `stuck_after` or `margin` falls back to its default; an invalid `max_age` value is dropped, so that interval keeps the cron-derived bound.

## Full example

```yaml
node: gc01srvr
friendly_name: GC01 srvr
parent: gc01hyper
hierarchy: grouped

sinks:
  - type: mqtt
    host: 192.168.1.65
    port: 1883
    username: mqtt
    password: ${MQTT_PASSWORD}
    base_topic: server-status
    discovery_prefix: homeassistant
    retain: false
    qos: 0
  - type: webhook
    url: https://n8n.example.com/webhook/server-status
    token: ${WEBHOOK_TOKEN}
    on_change: true

control:
  http:
    enabled: true
    bind: 127.0.0.1
    port: 9099
    token: ${CONTROL_TOKEN}

update:
  repo: giovi321/server-status-go
  check_interval_seconds: 21600

rsnapshot:
  stuck_after: 12h
  margin: 8h

disks:
  "WD-WCC4N7XXXXXX": data-1
smart_attributes: curated
```
