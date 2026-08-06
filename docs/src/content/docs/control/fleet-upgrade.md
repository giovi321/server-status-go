---
title: "Upgrading a fleet"
description: "Roll a new release across every host, without touching each one by hand"
---

[Self-update](../self-update/) covers what happens on one host. This page is the procedure for a fleet: cut a release once, then trigger the `update` command on every node.

Nothing here is a rolling deployment system. Each agent updates itself independently and there is no orchestrator, so a failed host stays on its old binary and everything else carries on.

## 1. Cut the release

Hosts self-update from GitHub Releases, so nothing happens until a release exists. Push a version tag:

```bash
git tag -a v1.0.3 -m "v1.0.3"
git push origin v1.0.3
```

The release workflow builds `server-status-linux-amd64` and `server-status-linux-arm64`, writes a `.sha256` next to each, and publishes the release. Wait for it to finish before triggering any host, or every one of them reports `already up to date`. See [Releasing](../../development/releasing/).

## 2. Trigger every host

### Over MQTT (the usual way)

One `mosquitto_pub` per node. Publish `1` to each node's `update` command topic:

```bash
NODES="gc01srvr gc01aisrvr gc01authsrvr gc01secsrvr gc01cloudsrvr"

for n in $NODES; do
  mosquitto_pub -h 192.168.1.65 -u mqtt -P "$PW" \
    -t "server-status/$n/cmd/update" -m 1
done
```

:::caution[Never publish retained]
Do not add `-r`. Retained command messages are deliberately ignored, so a retained `update` is dropped on arrival and would also re-fire on every future reconnect if it were not.
:::

Watch the results in a second terminal before you start. The result topics are non-retained, so subscribe first:

```bash
mosquitto_sub -h 192.168.1.65 -u mqtt -P "$PW" \
  -t 'server-status/+/cmd/update/result' -v
```

Each host answers once, with either `updated to v1.0.3, restarting` or `already up to date (v1.0.3)`.

### Over HTTP

For hosts with `control.http.enabled`, the same command is a POST. The token is required for commands, always:

```bash
curl -X POST -H "Authorization: Bearer $CONTROL_TOKEN" \
  http://<host>:9099/command/update
```

### From Home Assistant

Press **Install** on each host's **Agent** update entity (`update.<node>_agent`). This is per host, so it is the slow path on a large fleet, but it needs no shell.

### Over SSH, by reinstalling

Use this when the agent is not running, is too old to self-update, or the update command failed. The installer downloads the latest release, verifies its checksum, and restarts the service, keeping the existing config:

```bash
ssh <host> 'curl -fsSL https://raw.githubusercontent.com/giovi321/server-status-go/main/scripts/install.sh \
  | sudo bash -s -- --non-interactive'
```

`--non-interactive` matters on an already-configured host: it suppresses the setup prompts. Pin a specific build with `VERSION=v1.0.3` instead of the default `latest`.

## 3. Verify

Every agent republishes `agent_version` each cycle and its device `sw_version` on connect, so Home Assistant shows the new version on its own once each host restarts. To check from the shell:

```bash
mosquitto_sub -h 192.168.1.65 -u mqtt -P "$PW" \
  -t 'server-status/+/agent_version' -v -W 90
```

Any node still reporting the old tag either failed the update or was never triggered. Its journal has the reason:

```bash
journalctl -u server-status -n 50
```

## What to expect during the roll

- **Brief unavailability per host.** The update ends in `systemctl restart`, so the agent disconnects and its entities go unavailable until it reconnects and republishes `online`. Hosts are independent; there is no fleet-wide window
- **The version gate is a no-op, not an error.** A host already on the latest tag reports `already up to date` and does not touch its binary, so re-running the whole loop is safe
- **Failures are fail-closed.** A missing asset, a missing checksum, a mismatch, or any fetch error aborts with the running binary untouched. The previous binary is also kept as `server-status.bak` after a successful swap
- **A `dev` build always updates.** A binary built without the release ldflag reports `dev`, which never matches a tag, so it will always move to the latest release

## Rolling back

There is no rollback command. To put a host back on an older release, reinstall that version explicitly:

```bash
ssh <host> 'curl -fsSL https://raw.githubusercontent.com/giovi321/server-status-go/main/scripts/install.sh \
  | sudo VERSION=v1.0.2 bash -s -- --non-interactive'
```

The `.bak` next to the binary is the immediately previous build, and can be moved back into place by hand if the release assets are unavailable.
