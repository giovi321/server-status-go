---
title: "systemd and watchdog"
description: "The Type=notify unit, the sd_notify watchdog, and per-collector isolation"
---

The agent is built to run unattended. Under systemd it uses a `Type=notify` unit with a watchdog, and internally it isolates each collector so one slow or broken probe cannot take down the cycle.

## The unit

`packaging/server-status.service`, installed by `scripts/install.sh`:

| Directive | Value | Why |
|-----------|-------|-----|
| `Type` | `notify` | The agent signals readiness and liveness via sd_notify |
| `WatchdogSec` | `180` | systemd restarts the agent if it stops pinging for 180s |
| `User` | `root` | Needed for raw disk access (`smartctl`), `systemctl restart`, the docker socket, and hwmon |
| `EnvironmentFile` | `-/etc/server-status/server-status.env` | Loads `MQTT_PASSWORD` and similar. Leading `-` means a missing file is not fatal |
| `ExecStart` | `.../server-status -c /etc/server-status/config.yaml` | Runs at the default 60s interval |
| `Restart` | `always` | Restart on any exit |
| `RestartSec` | `15s` | Backoff between restarts |

## The watchdog

The agent speaks the systemd sd_notify protocol directly (a raw `unixgram` write to `$NOTIFY_SOCKET`, no cgo):

- On startup, after the sinks connect, it sends `READY=1`
- At the end of **every** cycle it sends `WATCHDOG=1`

If the process wedges (a cycle never completes), the pings stop, and systemd restarts the unit once `WatchdogSec` elapses. Off systemd, `$NOTIFY_SOCKET` is unset and the sd_notify calls are no-ops, so nothing changes when you run the binary by hand.

:::caution[Keep the interval under the deadline]
The watchdog is pet at the end of each cycle, so the gap between pings is roughly the publish interval plus the cycle time. Keep `--interval` well below `WatchdogSec` (the default 60s versus 180s leaves plenty of room). If you raise `--interval` to or past the deadline, the agent logs a warning at startup and systemd would restart it every cycle. Raise `WatchdogSec` in the unit if you deliberately want a long interval.
:::

## Per-collector isolation

Within a cycle, collectors run one after another, and each `Collect` is wrapped so a single misbehaving collector cannot stall or crash the run:

- **Timeout**. Each collector is bounded to 20 seconds. A hung probe (a stuck `smartctl`, a slow docker daemon) is abandoned and the cycle moves on. The timeout is derived from the run context, so `SIGINT`/`SIGTERM` still cancel sooner
- **Panic isolation**. A collector that panics is recovered; it contributes no metrics that cycle instead of taking down the process or the rest of the snapshot

Collectors run serially on purpose: several hold state between cycles (the network collector needs two samples to compute a rate), so running them concurrently would be unsafe. The 20 second per-collector bound is sized to keep even a bad cycle comfortably under the 180 second watchdog deadline.

## Shutdown

On `SIGINT`/`SIGTERM` the agent shuts down cleanly: the MQTT sink publishes `offline` and disconnects, so Home Assistant marks the host unavailable rather than showing stale values.
