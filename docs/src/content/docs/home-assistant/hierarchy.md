---
title: "Device hierarchy"
description: "Sub-devices for disks and docker, and nesting a VM under its host"
---

server-status models two independent kinds of parent-child relationship in Home Assistant, both through the `via_device` link.

<div class="diagram-frame">
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 980 300" role="img" aria-label="device hierarchy">
  <defs>
    <marker id="arrow-h" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto">
      <polygon points="0 0, 8 3, 0 6" fill="#57534e"/>
    </marker>
    <style>
      .b{font-family:'Geist','Inter',system-ui,sans-serif;font-weight:600;font-size:12px;fill:#1c1917;}
      .s{font-family:'Geist Mono','SF Mono',Menlo,monospace;font-size:9px;fill:#57534e;}
      .cap{font-family:'Instrument Serif',Georgia,serif;font-style:italic;font-size:13px;fill:#57534e;}
    </style>
  </defs>
  <rect width="100%" height="100%" fill="#faf7f2"/>
  <rect x="380" y="24" width="220" height="52" rx="6" fill="rgba(73,81,85,0.10)" stroke="#495155" stroke-width="1.2"/>
  <text x="490" y="46" class="b" text-anchor="middle">gc01hyper</text>
  <text x="490" y="63" class="s" text-anchor="middle">physical host (parent)</text>
  <line x1="490" y1="76" x2="490" y2="108" stroke="#57534e" marker-end="url(#arrow-h)"/>
  <text x="512" y="98" class="cap">parent:</text>
  <rect x="380" y="110" width="220" height="52" rx="6" fill="#fff" stroke="#495155" stroke-width="1.2"/>
  <text x="490" y="132" class="b" text-anchor="middle">gc01srvr</text>
  <text x="490" y="149" class="s" text-anchor="middle">VM host device</text>
  <g stroke="#57534e">
    <line x1="430" y1="162" x2="250" y2="212" marker-end="url(#arrow-h)"/>
    <line x1="490" y1="162" x2="490" y2="212" marker-end="url(#arrow-h)"/>
    <line x1="550" y1="162" x2="730" y2="212" marker-end="url(#arrow-h)"/>
  </g>
  <rect x="150" y="214" width="200" height="52" rx="6" fill="rgba(142,68,73,0.08)" stroke="#8E4449"/>
  <text x="250" y="236" class="b" text-anchor="middle">Disk data-1</text>
  <text x="250" y="253" class="s" text-anchor="middle">disk sub-device</text>
  <rect x="390" y="214" width="200" height="52" rx="6" fill="rgba(142,68,73,0.08)" stroke="#8E4449"/>
  <text x="490" y="236" class="b" text-anchor="middle">RAID md0</text>
  <text x="490" y="253" class="s" text-anchor="middle">array sub-device</text>
  <rect x="630" y="214" width="200" height="52" rx="6" fill="rgba(142,68,73,0.08)" stroke="#8E4449"/>
  <text x="730" y="236" class="b" text-anchor="middle">Docker</text>
  <text x="730" y="253" class="s" text-anchor="middle">docker sub-device</text>
</svg>
</div>

## Sub-devices (things inside a host)

Metrics for disks, RAID arrays, GPUs, ZFS pools, and docker are attached to their own Home Assistant device, linked back to the host with `via_device`. So a disk shows up as a device "GC01 srvr Disk data-1" nested under the host "GC01 srvr", grouping all of that disk's SMART entities together.

This is controlled by `hierarchy`:

```yaml
hierarchy: grouped   # default: sub-devices get their own HA device
# hierarchy: flat    # everything folds onto the single host device
```

- `grouped` (default) creates a sub-device per disk/array/GPU/pool plus one `Docker` sub-device
- `flat` suppresses the split, so every SMART, RAID, GPU, pool, and docker entity lives directly on the host device

A sub-device's `via_device` always points at its immediate host, never at a grandparent.

## Host under host (a VM under its hypervisor)

Set `parent` to another host's node to nest this host's device under that host's device:

```yaml
# on the VM
node: gc01srvr
parent: gc01hyper
```

The VM's device links `via_device` to `server-status-gc01hyper`. This is independent of `hierarchy`; it always applies when `parent` is set.

:::note[The parent must also run server-status]
`via_device` links to a device identifier of the form `server-status-<parent-node>`. For the nesting to render as a real parent in Home Assistant, that parent device must exist, which means the parent host should also run server-status. If it does not, Home Assistant keeps the VM as a top-level device.
:::

## Choosing a layout

- Leave `hierarchy: grouped` if you want disks and docker to have their own tidy device pages. This is the recommended default
- Use `flat` if you prefer a single flat device per host with every entity in one place
- Set `parent` only where the physical-to-virtual relationship is useful to see in Home Assistant
