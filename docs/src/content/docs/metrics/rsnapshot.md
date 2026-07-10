---
title: "Rsnapshot backups"
description: "Monitoring rsnapshot jobs from file evidence: entities, states, staleness bounds, and alert recipes"
---

The `rsnapshot` collector monitors rsnapshot backup jobs and answers one question per config file: is this backup chain alive? It never executes rsnapshot or any other binary. Every cycle it re-reads the evidence the job leaves behind:

- the config file itself (`snapshot_root`, retain levels, lock and log paths)
- the tail of the rsnapshot log (last 256 KiB, completion banners)
- the lockfile (pid, `/proc` liveness, cmdline pid-reuse guard)
- the interval directories under the snapshot root (`hoursago.0`, `daily.0`, ...)
- the snapshot root mount (exists, read-only)
- the cron sources that could schedule the job

The files are tiny, so there is no caching: what you see in Home Assistant is at most one cycle old.

## Discovery and naming

With no configuration the collector monitors `/etc/rsnapshot.conf` plus every `/etc/rsnapshot-*.conf`, skipping packaging and editor leftovers (`*.dpkg-dist`, `*.dpkg-old`, `*~`, `*.bak`).

Each config becomes one Home Assistant sub-device:

- `/etc/rsnapshot.conf` is named `main`
- `/etc/rsnapshot-gc01srvr.conf` is named `gc01srvr` (filename minus the `rsnapshot-` prefix and `.conf` suffix)
- the sub-device label is "Rsnapshot \<name\>", the component id is `rsnapshot-<name>`

Name collisions fall back to the conf basename, then to numbered suffixes. Listing `configs:` explicitly (see [Configuration](#configuration)) replaces auto-discovery entirely.

Entity ids follow the usual [naming scheme](../../home-assistant/naming/). For node `gc01srvr` and config `main`, the problem entity is `binary_sensor.gc01srvr_rsnapshot_main_rsnapshot_problem` (the hyphen in the component slugifies to an underscore).

## Entities

Text values are capped at 255 characters because Home Assistant drops longer states.

### Primary

| key | kind | device class | meaning |
|-----|------|--------------|---------|
| `rsnapshot_problem` | binary_sensor | problem | on when the state is `error`, `stale`, or `stuck`. The one entity to alert on |
| `rsnapshot_state` | text | | the evaluated state, see [States](#states) |
| `rsnapshot_last_result` | text | | outcome of the last run: `success`, `warnings`, `errors`, `running`, `died`, `unknown` |
| `rsnapshot_last_success` | sensor | timestamp | mtime of the lowest interval's `.0` directory, which rsnapshot touches on success. Omitted until the first successful run |
| `rsnapshot_stale` | binary_sensor | problem | the lowest `.0` is older than its staleness bound. Emitted only when a bound is known |
| `rsnapshot_stuck` | binary_sensor | problem | a live run has held the lock longer than `stuck_after` |
| `rsnapshot_interval_age` | sensor (h) | | age of each interval's `.0` directory, one entity per interval |

### Diagnostic

| key | kind | device class | meaning |
|-----|------|--------------|---------|
| `rsnapshot_running` | binary_sensor | running | a live rsnapshot process holds the lock |
| `rsnapshot_stale_lock` | binary_sensor | problem | lockfile exists but the pid is dead or unparseable, or the file stayed empty past a 5 minute grace |
| `rsnapshot_root_missing` | binary_sensor | problem | `snapshot_root` is not a readable directory (unmounted backup disk) |
| `rsnapshot_root_readonly` | binary_sensor | problem | the filesystem holding `snapshot_root` is mounted read-only |
| `rsnapshot_config_error` | binary_sensor | problem | conf unreadable, no `snapshot_root`, no retain levels, or a fault rsnapshot itself would reject (space-separated directives) |
| `rsnapshot_cron_jobs` | sensor | | number of cron lines attributed to this config |
| `rsnapshot_cron_list` | text | | the matched schedules, like `hoursago 0 */6 * * *; daysago 20 5 * * *` |
| `rsnapshot_intervals` | text | | retain levels in file order, like `hoursago:6 daysago:7 weeksago:4 monthsago:4` |
| `rsnapshot_stray_items` | sensor | | leftover `_delete.*` and `*.sync` entries in the snapshot root |
| `rsnapshot_details` | text | | one-line summary, like `mount:ok rw:ok conf:ok cron:4 lock:idle stray:0` |

### Host level

| key | kind | meaning |
|-----|------|---------|
| `rsnapshot_configs` | sensor | count of monitored configs, on the host device. Alert on it dropping: a deleted or renamed conf file silently removes its sub-device |

## States

`rsnapshot_state` is one enum evaluated top to bottom; the first match wins:

| state | meaning | problem |
|-------|---------|---------|
| `error` | config error, root missing, root read-only, stale lock, last run ended with errors, last run died (incomplete log and dead pid), or a partial cron schedule | yes |
| `stuck` | a live run has held the lock longer than `stuck_after` | yes |
| `stale` | the lowest `.0` is older than its bound, or no activity trace at all (log mtime, `.0` mtime, lock mtime) within the bound | yes |
| `running` | a live rsnapshot process holds the lock | no |
| `pending` | the lowest `.0` does not exist yet and nothing else is wrong (ramp-up: a fresh `monthsago` level takes months to appear) | no |
| `warning` | last run had non-benign warnings, or stray items sit in the snapshot root | no |
| `unknown` | no `.0` and the log or cron sources could not be read, so nothing can be asserted | no |
| `ok` | everything else | no |

`rsnapshot_problem` trips exactly when the state is `error`, `stale`, or `stuck`. Rsync "vanished file" warnings (exit code 24) are treated as benign: `rsnapshot_last_result` still reads `warnings`, but they do not trip the `warning` state.

A cron schedule that covers some intervals and misses others is an `error` (the reason names the unscheduled interval). Zero cron matches across all intervals is deliberately not a problem, since the schedule may live elsewhere (another host over ssh); `rsnapshot_cron_jobs` at 0 carries that signal.

### Lock handling

- an absent lockfile is idle and normal
- a pid is only considered alive when `/proc/<pid>` exists and its cmdline mentions rsnapshot, so a recycled pid never masks a dead run
- alive, and the lock mtime is older than `stuck_after`: `stuck`
- dead pid or unparseable content: stale lock, an `error`
- an empty lockfile younger than 5 minutes is ignored (rsnapshot writes the pid right after creating the file); older counts as a stale lock
- a live lock is the benign serialization case: `running`, never a problem

## Staleness

The freshness anchor is the mtime of `<snapshot_root>/<lowest interval>.0`. rsnapshot touches that directory on every successful run of the most frequent level, so it doubles as `rsnapshot_last_success`.

Only the lowest interval gates `stale` and therefore `problem`. Higher levels rotate by `mv`, which preserves mtime, so their `rsnapshot_interval_age` uses ctime and stays diagnostic.

The bound for the lowest interval is resolved in this order:

1. a `max_age` override from the config
2. cron-derived: the maximum gap between firings of the matched schedule, plus `margin`
3. none: no `rsnapshot_stale` entity is emitted and no staleness is asserted

A dead-man check backs this up: when a bound is known and the freshest of the log mtime, the lowest `.0` mtime, and the lock mtime is older than the bound, the state is `stale` even if the `.0` directory itself is missing. This catches a dead cron+wrapper chain that leaves no partial evidence.

## Cron schedules

The collector reads every source rsnapshot jobs realistically live in: the root user crontab (`/var/spool/cron/crontabs/root` or `/var/spool/cron/root`), `/etc/crontab`, and `/etc/cron.d/*` (skipping names with dots, as cron does). Comments and environment lines are ignored.

A cron line is attributed to a config when its command mentions rsnapshot and contains the conf path. This covers wrapper lines; the following attributes to `main` with interval `hoursago`:

```
0 */6 * * * flock -w 21600 /run/rsnapshot_serialize.lock timeout --signal=TERM --kill-after=10m 8h /home/programmi/rsnapshot_run.sh /etc/rsnapshot.conf hoursago /var/log/rsnapshot.log
```

A command mentioning rsnapshot with no `/etc/rsnapshot` path at all is attributed to the default conf (`/etc/rsnapshot.conf`) only. The interval is the retain name appearing as a whitespace-delimited token in the command.

For the staleness bound, entries from the root user crontab are authoritative: if any exist, other sources are ignored. Among the chosen entries the tightest maximum gap wins. The gap is the longest wait between consecutive firings, not the step:

| spec | max gap |
|------|---------|
| `0 */6 * * *` | 6h |
| `20 5 * * *` | 24h |
| `10 5 * * 1` | 7d |
| `0 5 1 * *` | 31d |

Lists, ranges, steps, month and weekday names, the standard dom/dow OR rule, and `@daily`-style aliases are supported. `@reboot` has no schedule and yields no bound.

## Configuration

Everything works with an empty config on hosts with `/etc/rsnapshot*.conf`. Overrides:

```yaml
rsnapshot:
  stuck_after: 12h   # a live run holding the lock longer than this is stuck
  margin: 8h         # slack added on top of cron-derived bounds
  configs:           # optional; listing any replaces auto-discovery
    - path: /etc/rsnapshot.conf
      name: main     # optional, overrides the derived sub-device name
      max_age:       # optional per-interval staleness overrides
        hoursago: 8h
    - path: /etc/rsnapshot-gc01srvr.conf
```

Durations use Go syntax (`90m`, `12h`, `36h`). `max_age` keys are retain names from the conf; the bound precedence is `max_age` over cron-derived (max gap + `margin`) over none, and only the lowest interval's bound gates `problem`.

## Alert recipes

Alert on the problem flag, with a short hold to ride out collector cycles:

```yaml
automation:
  - alias: "rsnapshot problem"
    triggers:
      - trigger: state
        entity_id: binary_sensor.gc01srvr_rsnapshot_main_rsnapshot_problem
        to: "on"
        for: "00:05:00"
    actions:
      - action: notify.mobile_app_phone
        data:
          message: >-
            rsnapshot main on gc01srvr is
            {{ states('sensor.gc01srvr_rsnapshot_main_rsnapshot_state') }}
            ({{ states('sensor.gc01srvr_rsnapshot_main_rsnapshot_details') }})
```

`unknown` is not a problem state, but an hour of it means the collector cannot see the log or cron sources and deserves a look:

```yaml
  - alias: "rsnapshot state unknown"
    triggers:
      - trigger: state
        entity_id: sensor.gc01srvr_rsnapshot_main_rsnapshot_state
        to: "unknown"
        for: "01:00:00"
    actions:
      - action: notify.mobile_app_phone
        data:
          message: "rsnapshot main on gc01srvr has been unknown for 1h"
```

A config file that disappears takes its sub-device down with it, so watch the host-level count:

```yaml
  - alias: "rsnapshot config disappeared"
    triggers:
      - trigger: state
        entity_id: sensor.gc01srvr_rsnapshot_configs
    conditions:
      - condition: template
        value_template: >-
          {{ trigger.from_state is not none
             and trigger.from_state.state not in ['unknown', 'unavailable']
             and trigger.to_state.state not in ['unknown', 'unavailable']
             and trigger.to_state.state | int(0) < trigger.from_state.state | int(0) }}
    actions:
      - action: notify.mobile_app_phone
        data:
          message: >-
            rsnapshot configs on gc01srvr dropped from
            {{ trigger.from_state.state }} to {{ trigger.to_state.state }}
```

## Near-real-time refresh after a run

The collector picks up a finished run on its next cycle. To see the result seconds after the run instead, keep the existing flock wrapper and let it poke the agent's [HTTP control API](../../control/http/) when the backup ends. Replace any legacy notification curl in the wrapper with:

```bash
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  http://127.0.0.1:9099/command/refresh
```

which needs the control server enabled:

```yaml
control:
  http:
    enabled: true
    bind: 127.0.0.1
    port: 9099
    token: ${CONTROL_TOKEN}
```

The refresh command queues an immediate publish cycle; endpoint and auth details are on the [HTTP control API](../../control/http/) page.

## Retiring the legacy scripts

If a host still runs a shell watchdog (`rsnapshot_watchdog.sh`) or posts run results to n8n from the wrapper, retire them in this order, one host at a time:

1. Deploy the agent with the collector and let both systems run in parallel for a few days. Compare what each reports after every scheduled run
2. Remove the `rsnapshot_watchdog.sh` cron line first. The collector's staleness and dead-man checks replace it entirely
3. Then remove the n8n POST from the wrapper, replacing it with the refresh curl above (or nothing, if next-cycle latency is acceptable)
4. Move to the next host only after the previous one has alerted correctly, or stayed silent correctly, through a few real runs
