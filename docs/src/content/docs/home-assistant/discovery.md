---
title: "Discovery and devices"
description: "How each host turns into a Home Assistant device with no manual YAML"
---

server-status uses Home Assistant's MQTT discovery. When a host connects, it publishes a retained discovery config for each entity to `<discovery_prefix>/<domain>/<node>/<object_id>/config`. Home Assistant picks these up and creates the device and its entities automatically. There is no `configuration.yaml` to edit.

## What gets created

For a host with node `gc01srvr` and friendly name "GC01 srvr":

- One **device** named "GC01 srvr", manufacturer `server-status`, with the running agent version as its software version
- A **sensor** or **binary sensor** per metric (text metrics are published as sensors)
- A **Refresh** and a **Restart** button, and an **Agent** update entity (see [MQTT commands](../../control/mqtt/))
- Sub-devices for disks, RAID arrays, GPUs, ZFS pools, and docker, when present (see [Device hierarchy](../hierarchy/))

Entities use `has_entity_name`, so Home Assistant shows short labels ("CPU usage", "Load 1m") under the device rather than repeating the host name in every entity.

## Attributes

Home Assistant refuses any state longer than 255 characters and drops the whole update, so a metric that carries a list or an inventory publishes it as entity attributes instead. Its discovery config gains a `json_attributes_topic` pointing at `<state topic>/attributes`, where the agent publishes a JSON object; Home Assistant exposes the object's keys as attributes on the entity. The state itself stays short — a count, or a clipped version of the text.

`docker_containers` is the main case: its state is the container count and its `containers` attribute holds the full inventory including per-container health and update flags. See the [metrics reference](../../metrics/reference/) for the others.

Upgrading an agent is all that is needed to switch an existing entity over. Discovery is retained and re-sent on every connect, so the new config with `json_attributes_topic` replaces the old one and the entity keeps its id and its history. No Home Assistant restart, and no change to any dashboard or automation that reads the entity.

## Availability

Every entity's `availability_topic` points at `<base>/<node>/availability`. The agent publishes `online` (retained) on connect and `offline` on clean shutdown; the MQTT last-will publishes `offline` if the connection drops unexpectedly. When a host is down, all its entities show as unavailable rather than holding a stale value.

## Retained and self-healing

Discovery configs are published retained, so the device and its entities survive a restart of either the broker or Home Assistant. On every reconnect the agent republishes availability and re-sends discovery. To remove a host cleanly, clear those retained configs with `--purge`; see [Clean uninstall](../../reliability/purge/).

## Requirements in Home Assistant

- The **MQTT integration** must be configured and pointed at the same broker
- Discovery must be enabled (it is on by default) under the same `discovery_prefix` the agent uses (`homeassistant` by default)

## Verifying

If a host does not appear:

1. Run `server-status -c config.yaml --dump-detected` to confirm the agent detects metrics
2. Check the agent logs for MQTT connection errors (`journalctl -u server-status -f`)
3. Confirm Home Assistant's MQTT integration is connected to the same broker, and that the discovery prefixes match
4. Look on the broker for the retained topics under `<discovery_prefix>/…/<node>/…/config` (see [MQTT topics](../../reference/topics/))
