---
title: "Self-update"
description: "How the agent updates its own binary from a GitHub release, safely"
---

The agent can replace its own binary with the latest GitHub release, verify the download against a published checksum, and restart, all triggered from Home Assistant or a control command. The update path is fail-closed: without a valid checksum it refuses to swap anything.

## Triggering an update

Any of these runs the `update` command:

- Press **Install** on the host's **Agent** update entity in Home Assistant
- Publish `1` to `server-status/<node>/cmd/update`
- `POST /command/update` on the [HTTP control API](../http/)

## What happens

<div class="diagram-frame">
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 980 220" role="img" aria-label="self-update flow">
  <defs>
    <marker id="arrow-su" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto">
      <polygon points="0 0, 8 3, 0 6" fill="#57534e"/>
    </marker>
    <style>
      .b{font-family:'Geist','Inter',system-ui,sans-serif;font-weight:600;font-size:12px;fill:#1c1917;}
      .s{font-family:'Geist Mono','SF Mono',Menlo,monospace;font-size:9px;fill:#57534e;}
    </style>
  </defs>
  <rect width="100%" height="100%" fill="#faf7f2"/>
  <g>
    <rect x="20" y="70" width="150" height="72" rx="6" fill="#fff" stroke="#8E4449" stroke-width="1.2"/>
    <text x="95" y="100" class="b" text-anchor="middle">Resolve latest</text>
    <text x="95" y="118" class="s" text-anchor="middle">GitHub API</text>
    <line x1="170" y1="106" x2="212" y2="106" stroke="#57534e" marker-end="url(#arrow-su)"/>
    <rect x="214" y="70" width="150" height="72" rx="6" fill="#fff" stroke="#57534e"/>
    <text x="289" y="100" class="b" text-anchor="middle">Download asset</text>
    <text x="289" y="118" class="s" text-anchor="middle">+ .sha256 sibling</text>
    <line x1="364" y1="106" x2="406" y2="106" stroke="#57534e" marker-end="url(#arrow-su)"/>
    <rect x="408" y="70" width="150" height="72" rx="6" fill="#fff" stroke="#57534e"/>
    <text x="483" y="100" class="b" text-anchor="middle">Verify sha256</text>
    <text x="483" y="118" class="s" text-anchor="middle">fail = abort</text>
    <line x1="558" y1="106" x2="600" y2="106" stroke="#57534e" marker-end="url(#arrow-su)"/>
    <rect x="602" y="70" width="150" height="72" rx="6" fill="#fff" stroke="#57534e"/>
    <text x="677" y="100" class="b" text-anchor="middle">Atomic swap</text>
    <text x="677" y="118" class="s" text-anchor="middle">keep .bak</text>
    <line x1="752" y1="106" x2="794" y2="106" stroke="#57534e" marker-end="url(#arrow-su)"/>
    <rect x="796" y="70" width="164" height="72" rx="6" fill="rgba(142,68,73,0.10)" stroke="#8E4449" stroke-width="1.2"/>
    <text x="878" y="100" class="b" text-anchor="middle">Restart service</text>
    <text x="878" y="118" class="s" text-anchor="middle">systemctl</text>
  </g>
</svg>
</div>

1. **Resolve the latest release** for `update.repo` (default `giovi321/server-status-go`) through the GitHub API, and locate the asset named `server-status-linux-<arch>` for this host's architecture plus its `.sha256` sibling
2. **Version gate**. If the latest tag equals the running version, stop and report `already up to date`
3. **Download** the asset (bounded to 512 MB) and its checksum
4. **Verify** the sha256. A mismatch, a missing checksum asset, or any fetch error aborts the update with the binary untouched
5. **Swap atomically**. Write the new binary, move the current one to `<path>.bak`, then rename the new one into place. If the rename fails, the backup is rolled back
6. **Restart** the service so the new binary takes over

## Safety properties

- **Fail-closed checksum**. The agent refuses to apply an update it cannot verify. There is no "skip verification" path
- **Bounded download**. The asset read is capped, and the checksum read is capped, so a hostile or broken endpoint cannot exhaust memory
- **Atomic swap with rollback**. The running binary is only replaced by an already-verified, already-written file, and the previous binary is kept as `.bak`
- **Serialized**. Concurrent update attempts cannot race the swap

## Requirements

- A release must exist on the configured repo with the arch-specific asset and its `.sha256` (the project's release workflow produces both). See [Releasing](../../development/releasing/)
- The service user must be able to overwrite `/opt/server-status/server-status` and run `systemctl restart`. The bundled unit runs as `root`

:::note[On-demand]
Updates run when you trigger them (button or command). `update.check_interval_seconds` is the configured check interval; the agent does not silently self-update in the background.
:::
