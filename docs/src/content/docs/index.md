---
title: "server-status"
description: "Single-binary host metrics agent for MQTT and Home Assistant"
---

<div style="text-align: center; margin-bottom: 1rem;">
  <img src="assets/logo.svg" alt="server-status" width="96" height="96" style="border-radius: 12px;" />
</div>

**Single-binary host metrics agent for MQTT and Home Assistant**

server-status runs on each of your Linux hosts, detects what hardware and software is present, and publishes the metrics that make sense for that host to an MQTT broker using Home Assistant discovery. Every host shows up in Home Assistant as its own device, with no manual entity configuration. The same snapshot can be POSTed to a webhook so a second consumer (for example n8n) sees exactly the same data.

It is a pure state publisher. Thresholds, alerts, and automations live in Home Assistant (or whatever consumes the webhook), not in the agent.

<div class="diagram-frame">
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 980 360" role="img" aria-label="server-status — detect, publish, appear in Home Assistant">
  <defs>
    <pattern id="dots-hero" width="22" height="22" patternUnits="userSpaceOnUse">
      <circle cx="1" cy="1" r="0.9" fill="rgba(28,25,23,0.10)"/>
    </pattern>
    <marker id="arrow-hero" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto">
      <polygon points="0 0, 8 3, 0 6" fill="#57534e"/>
    </marker>
    <marker id="arrow-hero-accent" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto">
      <polygon points="0 0, 8 3, 0 6" fill="#8E4449"/>
    </marker>
    <style>
      .eyebrow{font-family:'Geist Mono','SF Mono',Menlo,monospace;font-size:9px;letter-spacing:0.2em;fill:#78716c;}
      .name{font-family:'Geist','Inter',system-ui,sans-serif;font-weight:600;font-size:13px;fill:#1c1917;}
      .sub{font-family:'Geist Mono','SF Mono',Menlo,monospace;font-size:10px;fill:#57534e;}
      .label{font-family:'Geist Mono','SF Mono',Menlo,monospace;font-size:9px;fill:#57534e;letter-spacing:0.06em;}
      .label-accent{font-family:'Geist Mono','SF Mono',Menlo,monospace;font-size:9px;fill:#8E4449;letter-spacing:0.06em;}
      .title{font-family:'Instrument Serif',Georgia,serif;font-size:22px;fill:#1c1917;}
      .ital{font-family:'Instrument Serif',Georgia,serif;font-style:italic;font-size:14px;fill:#57534e;}
    </style>
  </defs>
  <rect width="100%" height="100%" fill="#faf7f2"/>
  <rect width="100%" height="100%" fill="url(#dots-hero)" opacity="0.6"/>
  <text x="40" y="44" class="eyebrow">SERVER-STATUS · OVERVIEW</text>
  <text x="40" y="76" class="title">Detect what a host has, publish it, watch it appear in Home Assistant</text>
  <line x1="208" y1="200" x2="288" y2="200" stroke="#57534e" stroke-width="1" marker-end="url(#arrow-hero)"/>
  <line x1="468" y1="200" x2="548" y2="200" stroke="#8E4449" stroke-width="1.2" marker-end="url(#arrow-hero-accent)"/>
  <line x1="728" y1="200" x2="808" y2="200" stroke="#57534e" stroke-width="1" marker-end="url(#arrow-hero)"/>
  <rect x="222" y="184" width="52" height="14" rx="2" fill="#faf7f2"/>
  <text x="248" y="194" class="label" text-anchor="middle">DETECT</text>
  <rect x="486" y="184" width="52" height="14" rx="2" fill="#faf7f2"/>
  <text x="512" y="194" class="label-accent" text-anchor="middle">PUBLISH</text>
  <rect x="742" y="184" width="52" height="14" rx="2" fill="#faf7f2"/>
  <text x="768" y="194" class="label" text-anchor="middle">DISCOVER</text>
  <rect x="40" y="140" width="168" height="120" rx="6" fill="rgba(87,83,78,0.10)" stroke="#78716c" stroke-width="1"/>
  <rect x="48" y="148" width="40" height="14" rx="2" fill="transparent" stroke="rgba(120,113,108,0.40)" stroke-width="0.8"/>
  <text x="68" y="158" font-family="'Geist Mono','SF Mono',Menlo,monospace" font-size="8" fill="rgba(120,113,108,0.9)" text-anchor="middle" letter-spacing="0.08em">HOST</text>
  <text x="124" y="190" class="name" text-anchor="middle">Linux host</text>
  <text x="124" y="208" class="sub" text-anchor="middle">cpu · disks · docker</text>
  <text x="124" y="232" class="ital" text-anchor="middle">one static binary</text>
  <rect x="288" y="120" width="180" height="160" rx="6" fill="rgba(142,68,73,0.10)" stroke="#8E4449" stroke-width="1.2"/>
  <rect x="296" y="128" width="64" height="14" rx="2" fill="transparent" stroke="rgba(142,68,73,0.50)" stroke-width="0.8"/>
  <text x="328" y="138" font-family="'Geist Mono','SF Mono',Menlo,monospace" font-size="8" fill="#8E4449" text-anchor="middle" letter-spacing="0.08em">COLLECTORS</text>
  <text x="378" y="168" class="name" text-anchor="middle">Auto-detected</text>
  <text x="378" y="186" class="sub" text-anchor="middle">cpu · mem · load · fs</text>
  <text x="378" y="200" class="sub" text-anchor="middle">smart · mdadm · zfs</text>
  <text x="378" y="214" class="sub" text-anchor="middle">gpu · docker · apt</text>
  <text x="378" y="238" class="ital" text-anchor="middle">only what's present</text>
  <rect x="548" y="140" width="180" height="120" rx="6" fill="#ffffff" stroke="#1c1917" stroke-width="1"/>
  <rect x="556" y="148" width="40" height="14" rx="2" fill="transparent" stroke="rgba(28,25,23,0.40)" stroke-width="0.8"/>
  <text x="576" y="158" font-family="'Geist Mono','SF Mono',Menlo,monospace" font-size="8" fill="rgba(28,25,23,0.8)" text-anchor="middle" letter-spacing="0.08em">SINKS</text>
  <text x="638" y="186" class="name" text-anchor="middle">MQTT + webhook</text>
  <text x="638" y="204" class="sub" text-anchor="middle">HA discovery</text>
  <text x="638" y="218" class="sub" text-anchor="middle">retained · LWT</text>
  <text x="638" y="240" class="ital" text-anchor="middle">same snapshot to both</text>
  <rect x="808" y="140" width="132" height="120" rx="6" fill="rgba(28,25,23,0.05)" stroke="#57534e" stroke-width="1"/>
  <rect x="816" y="148" width="24" height="14" rx="2" fill="transparent" stroke="rgba(87,83,78,0.40)" stroke-width="0.8"/>
  <text x="828" y="158" font-family="'Geist Mono','SF Mono',Menlo,monospace" font-size="8" fill="rgba(87,83,78,0.9)" text-anchor="middle" letter-spacing="0.08em">HA</text>
  <text x="874" y="186" class="name" text-anchor="middle">One device</text>
  <text x="874" y="204" class="sub" text-anchor="middle">per host</text>
  <text x="874" y="218" class="sub" text-anchor="middle">sub-devices</text>
  <text x="874" y="240" class="ital" text-anchor="middle">+ buttons</text>
  <line x1="40" y1="304" x2="940" y2="304" stroke="rgba(28,25,23,0.10)" stroke-width="0.8"/>
  <text x="40" y="324" class="ital">Self-hosted. One static Go binary. No agent-side database.</text>
  <text x="940" y="324" class="label" text-anchor="end">github.com/giovi321/server-status-go</text>
</svg>
</div>

## Key features

- **Autodetection**. Each collector reports only if the host actually has the thing it measures, so a VPS gets no temperature sensors and a machine without disks gets no SMART entities.
- **One Home Assistant device per host**. Every host publishes MQTT discovery for its own device; no manual entity YAML.
- **Device hierarchy**. Disks, RAID arrays, GPUs, and docker attach as sub-devices via `via_device`, and a host can be nested under a parent host (a VM under its hypervisor).
- **Broad hardware and software coverage**. CPU, memory, swap, load, uptime, filesystems, network, temperatures, APT updates, systemd failed units, SMART, mdadm, ZFS, GPU, and docker inventory plus update availability.
- **MQTT and webhook parity**. The same snapshot is published to MQTT and POSTed to a webhook, so a second consumer such as n8n sees identical data.
- **Self-update**. A Home Assistant button (or a control command) pulls the latest signed release from GitHub, verifies its checksum, and swaps the binary atomically.
- **Control surface**. Refresh, restart, and update on demand over HTTP or MQTT, plus a read-only HTTP snapshot endpoint.
- **Reliable by default**. Per-collector timeouts and panic isolation, a systemd sd_notify watchdog, and a `--purge` uninstall that removes the host from Home Assistant cleanly.
- **Single static binary**. No runtime dependencies, no agent-side database. Built for Debian and Ubuntu, amd64 and arm64.

## Quick start

```bash
# Build (Go 1.22+), or download a release binary
go build -o server-status ./cmd/server-status

# Point it at your broker and run one cycle
cat > config.yaml <<'YAML'
node:            # defaults to the hostname
sinks:
  - type: mqtt
    host: 192.168.1.65
    username: mqtt
    password: ${MQTT_PASSWORD}
YAML

MQTT_PASSWORD=secret ./server-status -c config.yaml --once
```

The host appears in Home Assistant as a device named after its node. For a permanent install as a systemd service, see [Installation](getting-started/installation/).

:::tip[See what a host would publish]
Run `server-status -c config.yaml --dump-detected` to print, as JSON, every collector, whether it is available on this host, and the exact metrics it would send. No broker connection required.
:::

## What it publishes

| Area | Examples |
|------|----------|
| Core | CPU usage, memory, swap, load, uptime |
| Storage | Per-mount filesystem usage and inodes, SMART attributes, mdadm arrays, ZFS pools |
| Network | Per-interface throughput and link state |
| Software | APT updates and security updates, reboot-required, systemd failed units |
| Hardware | Temperatures (hwmon), GPU utilization and memory |
| Docker | Running/stopped/unhealthy counts, containers with updates available |
| Agent | Last-seen heartbeat and agent version |

See the [Metrics reference](metrics/reference/) for every metric key, unit, and entity type.

## Architecture at a glance

server-status is a single Go binary. On each cycle it builds one **snapshot** by running every available **collector**, then hands that snapshot to each configured **sink** (MQTT and/or webhook). Collectors are pure readers of `/proc`, `/sys`, and a few standard CLIs; sinks own transport concerns such as Home Assistant discovery, retained topics, and the webhook POST.

See [Collectors](metrics/collectors/) and [MQTT topics](reference/topics/) for the full picture.
