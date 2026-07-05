---
title: "HTTP control API"
description: "The read-only snapshot endpoint and command dispatch over HTTP"
---

When `control.http.enabled` is `true`, the agent serves a small HTTP surface: a health check, the latest snapshot, and command dispatch. It binds synchronously at startup, so a port clash is reported immediately rather than failing silently.

```yaml
control:
  http:
    enabled: true
    bind: 127.0.0.1
    port: 9099
    token: ${CONTROL_TOKEN}
```

:::caution[Bind locally]
This is a control surface. Bind it to `127.0.0.1` (or a trusted management subnet) and set a token. Do not expose it to untrusted networks.
:::

## Endpoints

| Method | Path | Auth | Success | Errors |
|--------|------|------|---------|--------|
| `GET` | `/health` | none | `200` `{"status":"ok","version":"..."}` | |
| `GET` | `/snapshot` | token if set | `200` snapshot JSON | `401` bad token, `503` no snapshot yet |
| `POST` | `/command/{name}` | token required | `200` `{"ok":true,...}` | `401` bad token, `403` no token configured, `400` command failed, `503` commands unavailable |

## Authentication

Auth is a bearer token from `control.http.token`, sent as `Authorization: Bearer <token>`.

- `/health` is always open
- `/snapshot` is open when no token is set, and token-gated when one is
- `/command/{name}` **always requires a token**. If no token is configured it returns `403`, so commands can never be triggered on an unauthenticated control server

## Health

```bash
curl http://127.0.0.1:9099/health
# {"status":"ok","version":"v1.0.0"}
```

## Snapshot

Returns the most recent published snapshot (the same JSON the [webhook sink](../webhook/) POSTs).

```bash
curl -H "Authorization: Bearer $CONTROL_TOKEN" http://127.0.0.1:9099/snapshot
```

Returns `503 no snapshot yet` before the first cycle completes.

## Commands

`{name}` is one of the registered commands: `refresh`, `restart`, `update` (see [MQTT commands](../mqtt/) for what each does; the dispatcher is shared).

```bash
# Trigger an immediate publish cycle
curl -X POST -H "Authorization: Bearer $CONTROL_TOKEN" \
  http://127.0.0.1:9099/command/refresh
# {"ok":true,"message":"refresh queued"}

# Unknown command -> 400
curl -X POST -H "Authorization: Bearer $CONTROL_TOKEN" \
  http://127.0.0.1:9099/command/nope
# {"ok":false,"message":"unknown command: nope"}
```

The response body is always `{"ok":bool,"message":string}`. A failed command (`ok:false`) is returned with status `400`.
