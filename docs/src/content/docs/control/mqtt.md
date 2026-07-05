---
title: "MQTT commands"
description: "Trigger refresh, restart, and update over MQTT"
---

When an MQTT sink is configured, the agent subscribes to a command topic for its node and exposes the same three commands as the [HTTP control API](../http/). It also publishes Home Assistant button and update entities so you can trigger them from a dashboard.

## Command topics

```
<base>/<node>/cmd/<command>            (inbound, send payload "1")
<base>/<node>/cmd/<command>/result     (outbound, JSON {ok, message})
```

Publish `1` to the command topic; the agent runs it and publishes the result (non-retained) to the `/result` subtopic. Retained messages on command topics are ignored, so a stale retained command cannot re-fire when the agent reconnects.

```bash
mosquitto_pub -h 192.168.1.65 -u mqtt -P "$PW" \
  -t server-status/gc01srvr/cmd/refresh -m 1
```

## The commands

| Command | Effect | Result message |
|---------|--------|----------------|
| `refresh` | Run a collect-and-publish cycle now, instead of waiting for the next interval | `refresh queued` |
| `restart` | `systemctl restart server-status` on the host | `restarting` |
| `update` | Check for a newer release and, if there is one, download, verify, swap, and restart. See [Self-update](../self-update/) | `updated to <version>, restarting`, or `already up to date (<version>)` |

An unknown command returns `{"ok":false,"message":"unknown command: <name>"}`.

## Home Assistant entities

On connect the agent publishes discovery for, on the host device:

- a **Refresh** button (`button.<node>_refresh`)
- a **Restart** button (`button.<node>_restart`)
- an **Agent** update entity (`update.<node>_agent`) whose Install action maps to the `update` command

Pressing a button publishes `1` to the matching command topic. These entities are created whenever an MQTT sink is present; there is no separate switch to enable them.

:::note[restart and update need privilege]
Both shell out to `systemctl`. The bundled systemd unit runs as `root`, so they work out of the box. If you run the agent as a non-root user, it must be allowed to restart its own service.
:::
