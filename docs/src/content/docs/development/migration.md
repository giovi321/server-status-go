---
title: "Migrating from the Python tool"
description: "Cut over from the archived Python server-status, one host at a time"
---

This Go agent replaces the archived Python `server-status`. The two publish under different MQTT devices, so they do not collide, and you can migrate one host at a time with no flag day.

## Cut over, per host

1. **Install the Go agent** on the host (see [Installation](../../getting-started/installation/)). It publishes a new per-host device keyed by `node` (the hostname by default), so it will not overwrite or clash with the old Python device
2. **Verify** the new device and its entities appear in Home Assistant and read correctly (`--dump-detected` helps confirm detection)
3. **Stop and remove the old Python service** on that host: `systemctl disable --now server-status` for the old unit, then delete its files
4. **Delete the old Python MQTT device** in Home Assistant, since it is now stale. If the Python tool used retained discovery, clear those retained topics on the broker so the device does not reappear
5. **Repeat** for the next host

## Removing the Go agent later

To fully remove the Go agent from a host, including its Home Assistant device:

```bash
sudo ./scripts/install.sh --uninstall
```

This stops the service, runs `server-status --purge` to clear the retained MQTT discovery (so the Home Assistant device disappears), and removes the systemd unit. It leaves `/etc/server-status` and `/opt/server-status` in place; delete them by hand to remove the config and binary too. See [Clean uninstall](../../reliability/purge/).

## What changes for consumers

- **Entity ids may differ** from the Python tool's, since naming is derived from `node` and the metric keys documented in the [Metrics reference](../../metrics/reference/). Update any automations or dashboards that referenced the old entity ids
- **Webhook parity**. If you consumed the Python tool over a webhook, point the same consumer at the Go agent's [webhook sink](../../control/webhook/); the payload is the full snapshot JSON
