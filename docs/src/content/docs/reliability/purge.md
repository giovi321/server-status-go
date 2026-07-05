---
title: "Clean uninstall"
description: "What --purge does and why it removes a host from Home Assistant cleanly"
---

Because discovery configs are published retained, they outlive the agent. If you just stop and delete the agent, Home Assistant keeps showing the device (now permanently unavailable) until you clear those retained topics by hand. `--purge` does that for you.

```bash
server-status -c config.yaml --purge
```

The uninstaller runs this automatically; see [Installation → Uninstall](../../getting-started/installation/#uninstall).

## What it does

1. **Sweep**. Subscribe to the wildcard `<discovery_prefix>/+/<node>/+/config`, which matches the host device and every sub-device's discovery topics regardless of entity domain. Wait briefly for the broker to deliver the retained configs, collect every topic that arrives with a non-empty payload, then clear each one with an empty retained payload. Because this reads what is actually retained on the broker, it also clears **orphans**: entities from earlier cycles that are no longer in the current snapshot, such as an unplugged disk or a removed container
2. **Belt-and-suspenders**. Also explicitly clear the discovery topics for every metric in the current snapshot plus the three control entities (the two buttons and the update entity), in case the retained delivery in step 1 was slow or incomplete
3. **Availability**. Clear the availability topic
4. **Graceful disconnect**. Disconnect cleanly so the MQTT last-will does not fire, which would otherwise republish a retained `offline` onto the availability topic that was just cleared

The result: Home Assistant removes the device and all of its entities, with nothing left retained on the broker for that node.

:::note[Stop the service first]
The uninstaller stops the service before purging so the running agent and the purge do not fight over the shared MQTT client id. If you run `--purge` by hand against a host whose service is still running, stop the service first (`systemctl stop server-status`).
:::

## When to use it

- As part of decommissioning a host (the uninstaller does this)
- After changing a host's `node`, to remove the entities published under the old node
- To clear a test device from Home Assistant

## What it does not touch

`--purge` only clears MQTT retained state for this node. It does not remove the binary, the config, or the systemd unit. The webhook sink has no retained state, so `--purge` does nothing for a webhook-only configuration.
