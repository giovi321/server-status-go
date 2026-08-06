---
title: "MQTT topics"
description: "The exact topic layout for state, availability, discovery, and commands"
---

Everything the MQTT sink publishes lives under two roots: the `base_topic` (default `server-status`) for state, availability, and commands, and the `discovery_prefix` (default `homeassistant`) for Home Assistant discovery configs. The examples below use node `gc01srvr`.

## State

```
<base>/<node>[/<component>]/<key>[/<instance>]
```

- Host metric: `server-status/gc01srvr/cpu_usage`
- Multi-instance metric: `server-status/gc01srvr/fs_usage/mnt-data` (the instance is slugified to `[a-z0-9-]`)
- Sub-device metric: `server-status/gc01srvr/<component>/<key>[/<instance>]`

State values are plain strings. Binary sensors use `ON`/`OFF`. State is retained only when the sink's `retain` is `true` (default `false`).

Home Assistant discards a state longer than 255 characters, so string states are clipped to that length on publish.

## Attributes

```
<base>/<node>[/<component>]/<key>[/<instance>]/attributes
```

Metrics that carry more detail than a 255-character state can hold publish a JSON object here, and their discovery config points `json_attributes_topic` at it. Home Assistant surfaces the object's keys as entity attributes, which have no length limit. Example: `server-status/gc01srvr/docker/docker_containers/attributes`.

The payload is always a JSON object, never a bare array, so no `json_attributes_template` is involved. It follows the same `retain` setting as state. Metrics without attributes publish nothing here and omit `json_attributes_topic` entirely.

## Availability

```
<base>/<node>/availability     ->  online | offline
```

`server-status/gc01srvr/availability`. Published `online` (retained) on connect, and `offline` (retained) on clean shutdown or via the MQTT last-will if the connection drops. Every entity's discovery points its `availability_topic` here.

## Discovery config

```
<discovery_prefix>/<component>/<node>/<object_id>/config
```

- `component` is one of `sensor`, `binary_sensor`, `button`, `update`
- `object_id` is `<node>_<key>`, or `<node>_<component>_<key>` for sub-device entities, with the instance slug appended when present

Examples:

```
homeassistant/sensor/gc01srvr/gc01srvr_cpu_usage/config
homeassistant/sensor/gc01srvr/gc01srvr_fs_usage_mnt-data/config
homeassistant/binary_sensor/gc01srvr/gc01srvr_reboot_required/config
homeassistant/button/gc01srvr/gc01srvr_cmd_refresh/config
homeassistant/update/gc01srvr/gc01srvr_agent/config
```

Discovery configs are always published retained, so entities survive a broker or Home Assistant restart. They are re-sent once per broker connection. Clearing them (an empty retained payload) is exactly what `--purge` does. See [Clean uninstall](../../reliability/purge/).

## Commands

```
<base>/<node>/cmd/<command>            (inbound, payload "1")
<base>/<node>/cmd/<command>/result     (outbound, JSON {ok, message})
```

Publishing `1` to `server-status/gc01srvr/cmd/refresh` triggers the command; the agent publishes the result (non-retained) to `.../cmd/refresh/result`. Retained command messages are ignored, so a stale retained command can never re-fire on reconnect. See [MQTT commands](../../control/mqtt/).

## Agent update entity

The update entity's discovery sets its state topic to:

```
<base>/<node>/agent/update            JSON {installed_version, latest_version}
```

The Install action maps to the `update` command topic.

The payload is published **retained**, so the entity has a version the moment Home Assistant subscribes rather than showing unknown until the next check:

```json
{
  "installed_version": "v1.0.3",
  "latest_version": "v1.0.4",
  "title": "server-status v1.0.4",
  "release_url": "https://github.com/giovi321/server-status-go/releases/tag/v1.0.4"
}
```

`installed_version` is the running build. `latest_version` comes from the GitHub release check, refreshed every `update.check_interval_seconds` (default 6 hours) on a goroutine separate from the publish cycle, so a slow GitHub response delays neither a publish nor the watchdog ping. Before the first successful check, `latest_version` is omitted rather than sent empty, which Home Assistant reads as "not known yet". A failed check keeps the last known value instead of blanking it.

`--purge` clears this topic along with the discovery configs.

## Connection behavior

| Aspect | Value |
|--------|-------|
| Client id | `server-status-<node>` |
| Keepalive | 30s |
| Last will | `availability` topic, `offline`, retained |
| Auto-reconnect | on, 5s retry, capped at 60s |
| On (re)connect | republish `online`, re-subscribe commands, re-send discovery |

Because the client id is derived from the node, do not run two agents with the same node against one broker; the second connection displaces the first.
