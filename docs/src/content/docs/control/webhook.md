---
title: "Webhook sink"
description: "POST the same snapshot to an HTTP endpoint, for n8n and other consumers"
---

The webhook sink POSTs each snapshot as JSON to a URL. It consumes the exact same `Snapshot` the MQTT sink does, so the two are metric-for-metric identical. This is the path for feeding a second consumer such as n8n without giving up the Home Assistant integration.

```yaml
sinks:
  - type: webhook
    url: https://n8n.example.com/webhook/server-status
    token: ${WEBHOOK_TOKEN}   # optional
    on_change: true            # optional
```

MQTT and webhook can be configured together; both receive every cycle's snapshot.

## Request

- `POST` to `url`
- `Content-Type: application/json`
- `Authorization: Bearer <token>` when `token` is set
- Body: the full snapshot as JSON

## Payload shape

```json
{
  "device": {
    "node": "gc01srvr",
    "name": "GC01 srvr",
    "identifier": "server-status-gc01srvr",
    "parent": "gc01hyper",
    "hierarchy": "grouped",
    "manufacturer": "server-status",
    "sw_version": "v1.0.0"
  },
  "ts": "2026-07-06T00:00:00Z",
  "metrics": [
    {
      "key": "cpu_usage",
      "name": "CPU usage",
      "value": 12.4,
      "unit": "%",
      "device_class": "",
      "state_class": "measurement",
      "kind": "sensor",
      "category": "primary"
    },
    {
      "key": "fs_usage",
      "instance": "/mnt/data",
      "name": "/mnt/data usage",
      "value": 63.1,
      "unit": "%",
      "kind": "sensor",
      "category": "primary"
    }
  ]
}
```

Each metric carries the same fields the MQTT discovery is built from: `key`, `component` and `component_name` (for sub-devices), `instance` (for multi-instance metrics), `name`, `value`, `unit`, `device_class`, `state_class`, `kind`, `category`, and `icon`. Absent fields are omitted. See the [Metrics reference](../../metrics/reference/) for the full catalog.

## Delivery

- Up to **3 attempts** per cycle, with a short backoff between tries
- Success is any `2xx` response; the body is drained and discarded
- On persistent failure the cycle's POST is dropped and the next cycle tries again

## on_change

With `on_change: true`, the sink compares the metrics (ignoring the timestamp) against the last **successfully delivered** payload and skips the POST when nothing changed. Because the comparison baseline only advances after a `2xx`, a failed delivery is retried on the next cycle rather than silently swallowed. Leave it off to POST every cycle (a steady heartbeat); turn it on to cut traffic to change-only.
