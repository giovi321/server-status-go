---
title: "CLI flags"
description: "Every command-line flag server-status accepts"
---

server-status is invoked as `server-status [flags]`. Every mode except `--version` requires a config with `-c`/`--config`.

| Flag | Default | Description |
|------|---------|-------------|
| `-c`, `--config` | (required) | Path to the YAML config file. Without it, the agent exits with `missing -c/--config` |
| `--once` | `false` | Run exactly one collect-and-publish cycle, then exit |
| `--interval` | `60` | Seconds between cycles in the run loop |
| `--dump-detected` | `false` | Print detected collectors and the metrics they would publish as JSON, then exit. Does not connect to any sink |
| `--purge` | `false` | Connect and clear this host's retained MQTT discovery (removing it from Home Assistant), then exit |
| `--version` | `false` | Print the build version and exit |

Flags use Go's flag parser, so both `-flag` and `--flag` forms work (for example `-once` and `--once` are equivalent). `--config` and `-c` are the same option.

## Common invocations

```bash
# First smoke test against the broker
server-status -c config.yaml --once

# See what this host detects, no broker needed
server-status -c config.yaml --dump-detected

# The run loop (what systemd runs)
server-status -c config.yaml --interval 60

# Remove this host from Home Assistant cleanly
server-status -c config.yaml --purge

# Version
server-status --version
```

## Exit behavior

- `--version` prints the version and returns immediately
- A missing or unreadable config is a fatal error
- If every configured sink fails to connect, the agent exits with a fatal error rather than running blind
- In the run loop, `SIGINT`/`SIGTERM` trigger a clean shutdown: the MQTT sink publishes `offline` and disconnects
