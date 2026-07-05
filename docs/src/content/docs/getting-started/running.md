---
title: "Running the agent"
description: "Run one cycle, preview detection, or run the loop, from the command line"
---

Under systemd the agent runs continuously and you rarely invoke it by hand. During setup and debugging, a few flags are useful. All of them take the same `-c` config.

## One cycle, then exit

```bash
server-status -c config.yaml --once
```

Connects the configured sinks, publishes one snapshot, and exits. Good for a first smoke test against the broker. On exit the MQTT sink publishes an `offline` availability, so entities show as unavailable until the loop runs.

## Preview what a host detects

```bash
server-status -c config.yaml --dump-detected
```

Prints JSON: the host device block, then every collector with whether it is available on this host and the exact metrics it would publish. This never connects to the broker, so it is safe to run anywhere. Use it to answer "why is this entity missing?" (the collector is not available) or "what key does that sensor use?".

## Run the loop

```bash
server-status -c config.yaml --interval 60
```

Runs a cycle immediately, then every `--interval` seconds (default 60). This is what the systemd unit does. `SIGINT`/`SIGTERM` shut it down cleanly (the MQTT sink publishes `offline` and disconnects).

:::caution[Interval and the watchdog]
Under systemd the unit sets a watchdog deadline (`WatchdogSec=180`). Keep `--interval` comfortably below it. If you raise the interval to or beyond the deadline, the agent logs a warning at startup and systemd would restart it every cycle. See [systemd and watchdog](../../reliability/systemd/).
:::

## Clear a host from Home Assistant

```bash
server-status -c config.yaml --purge
```

Connects, clears all of this host's retained MQTT discovery (so Home Assistant removes the device and every entity), then exits. Used by the uninstaller. See [Clean uninstall](../../reliability/purge/).

## Print the version

```bash
server-status --version
```

See the [CLI flags](../../reference/cli/) reference for the full list.
