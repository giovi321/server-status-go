# rsnapshot status: per-group results, run stats and a Home Assistant view

Status: approved for planning
Date: 2026-10-31
Author: giovi321

## 1. Summary

server-status-go already publishes one Home Assistant device per rsnapshot config. Since 2026-10-31 both backup hosts run several configs (GC01SRVR: `rsnapshot-{a,b,c}`; GC03SRVR: `rsnapshot-{a,b,c,vm}` and `rsnapshot-gc01srvr`), and that change breaks part of what the collector reports. This design fixes the collector, adds per-run statistics (duration, data transferred, logical backup size) without walking the snapshot trees, and adds a Home Assistant dashboard that shows the state of every group on both hosts.

The work spans three places, and nothing new is created outside them:

- `giovi321/server-status-go`: collector fixes and new entities
- `giovi321/server-scripts`: the `rsnapshot_run.sh` wrapper writes a per-run state file; the rsnapshot configs add `--stats`
- Home Assistant: one dashboard, captured into `server-scripts` by the existing nightly HA config capture

## 2. Why

- Giovanni wants one place that shows, per host and per group: what failed, when the last good backup was, how long it took and how big it is
- The groups now share `/var/log/rsnapshot.log`, and the collector attributes the newest run in that file to every config. A failed group C run followed by a successful group A run shows as "success" on both devices, so a failure can be hidden. The failure alerts are not affected: the webhook runs inside the shared flock, straight after its own run, so the newest block in the log is always its own
- The log timestamps are ISO (`[2026-10-31T04:19:37]`, rsnapshot 1.5.1), and the collector only parses `[10/Jul/2026:05:20:01]`, so run start and end times are never parsed
- There is no size data at all today

## 3. Goals

- Each config's device reports only its own runs
- Per config: last run result, last run time, last run duration, last exit code, data transferred by the last sync, logical size of the last sync, files transferred
- Works on both hosts, for every config the collector already discovers, with no per-host configuration
- A Home Assistant view where a failed or overdue group is visible at a glance

## 4. Non-goals

- No alerting changes. Failure alerts stay with the wrapper's webhook and the n8n workflows `rsnapshot-log-gc01srvr` / `rsnapshot-log-gc03srvr`; staleness stays with `rsnapshot-stale.timer`. Home Assistant only displays
- No custom web UI
- No true on-disk size per snapshot. With hardlinks it needs a `du` walk across slots, and metadata walks on GC01SRVR's md2 are banned (Appendix C #44: a concurrent walk pushed `hoursago` runs past the 8h cap)

## 5. Anti-goals

- Anything that walks snapshot trees or the landing folders on a schedule
- A device that shows "success" on evidence that belongs to another config
- A new standalone script or service outside the three places in section 1

## 6. Design

### 6.1 Collector: attribute log blocks to their config

`classifyRsnapshotLog` gains the config path as input. A `started` banner reads `/usr/bin/rsnapshot -c /etc/rsnapshot-a.conf hoursago: started`. Rules:

- A banner with `-c <path>` belongs to that path only
- A banner without `-c` belongs to `/etc/rsnapshot.conf` only (rsnapshot's default config)
- Lines between a block's `started` and its completion banner belong to that block. Runs are serialised by the shared flock `/run/rsnapshot_serialize.lock`, so blocks never interleave on these hosts. The builder decides how to behave if they ever do, as long as it never credits one config with another's result

The same filter applies to everything else that reads the log: the dead-man freshness check must use the newest line of this config's own blocks, not the file's mtime, since another group writing the log keeps the mtime fresh.

### 6.2 Collector: timestamps

Parse both `[2026-10-31T04:19:37]` (rsnapshot 1.5.x) and `[10/Jul/2026:05:20:01]` (older). Banner times carry no zone; read them in the host's local zone.

### 6.3 Wrapper: per-run state file

`rsnapshot_run.sh` (GC01SRVR and GC03SRVR copies) captures rsnapshot's stdout and writes, after every run, `/var/lib/rsnapshot-status/<conf basename without .conf>.<interval>.json`, for example `rsnapshot-a.hoursago.json`. Written atomically (temp file plus `mv`), world-readable, directory created if missing. Fields:

| field | meaning |
|---|---|
| `config` | full config path |
| `interval` | interval name |
| `start`, `end` | RFC 3339 with zone |
| `exit_code` | rsnapshot's exit code, or 124/137 from `timeout` |
| `synced` | true when the run did a file sync (the lowest interval), false for rotate-only runs |
| `bytes_transferred` | sum of rsync `Total transferred file size` over all backup points |
| `bytes_total` | sum of rsync `Total file size`: the logical size of the data in this snapshot |
| `files_transferred` | sum of `Number of regular files transferred` |
| `backup_points` | how many rsync `--stats` blocks were summed |

`synced` means the run was the lowest interval, so a file sync was attempted, whatever its outcome. The stats fields are present only when `synced` is true and at least one stats block was parsed.

A second file, `<conf basename without .conf>.last-sync.json`, with the same fields, is written only when a `synced` run exits 0 or 2. Exit 2 is rsnapshot's "completed with warnings", which the units already treat as success (`SuccessExitStatus=2`). A failed or killed sync therefore never overwrites the last good stats, and partial sums from a run that died halfway never reach the size history. (Design call, 2026-10-31: the size and transfer entities hold the last good sync; the failure itself shows through the last run entities.)

Writing either file must never change the wrapper's exit code, and the webhook call stays as it is.

The rsnapshot configs in `server-scripts` (both hosts, all groups and `rsnapshot-gc01srvr.conf`) set `rsync_long_args` to rsnapshot's default plus `--stats`: `--delete --numeric-ids --relative --delete-excluded --stats`. Group C's per-backup-point `+rsync_long_args` appends to it and stays as is.

### 6.4 Collector: read the state files

For each discovered config, read `/var/lib/rsnapshot-status/<name>.*.json`. New entities on the config's existing device:

- Last run (timestamp), last run interval (text), last run duration (s), last exit code
- From the per-interval files, the newest by `end`: last run (timestamp), interval, duration, exit code
- From `<name>.last-sync.json` only: last good sync transferred (bytes), last good sync size (bytes, `bytes_total`), last good sync files transferred, last good sync time

Byte entities use `device_class: data_size` and `state_class: measurement` so Home Assistant keeps long-term statistics. When a state file and the log disagree about the newest run, the newer evidence wins. A missing state directory means the entities are not published, which matches the collector's "only publish what has data" rule.

### 6.5 Stale "main" devices

Retiring `/etc/rsnapshot.conf` left a "Rsnapshot main" device on both hosts in Home Assistant. Remove it. Whether the collector should clear discovery for configs that disappear is the builder's call; at minimum, remove these two by hand.

### 6.6 Home Assistant dashboard

A "Backups" view (or dashboard, builder's call) with:

- One section per host, one card per group: status coloured ok / warnings / failed / overdue, time since last success, last duration, last sync transferred, last sync size, age of each interval level
- 30-day history graphs per group: transferred bytes, logical size, duration
- Snapshot disk usage per host, from the existing filesystem entities (`/media/backup` on GC01SRVR, `/media/storage1` on GC03SRVR)

Built with core cards if they are enough; custom cards only where core cards cannot show it. The dashboard is captured into `server-scripts` by the nightly HA capture.

## 7. Constraints

- Hard rule: nothing in this design may walk snapshot trees or landing folders
- GC03SRVR is off-site and self-deploys from its own `server-scripts` clone; changes there reach it as a command Giovanni runs. GC01SRVR can be deployed directly
- Commits and PRs are attributed to giovi321 only; squash-merge to main
- server-scripts changes go through `scripts-deployer.sh` and pass `lint.sh`; `rsnapshot configtest` must pass for every changed config
- The collector keeps working against hosts without state files (older wrapper) and against single-config hosts

## 8. Left to the builder

- Parsing approach for the stats in bash (awk or grep), and number formats (rsync prints thousands separators)
- Entity names and keys, following the collector's existing naming
- How the server-status-go release reaches both hosts, following the existing install and self-update path
- Test fixtures: use a real excerpt of a shared multi-group `/var/log/rsnapshot.log`
- The 256 KB log tail can lose a daily group's block when several groups write the log at loglevel 3: read a larger tail, scan backwards until this config's last block is found, or fall back to the state files
- Whether a new dashboard needs a line in `GC01haSRVR/capture.manifest` (only `dashboard_admin` is captured today; a view inside it is captured automatically)

## 9. Order

1. Collector fixes 6.1 and 6.2, with tests, released and deployed. This alone makes the existing "last result" trustworthy
2. Wrapper and `--stats` (6.3), deployed on GC01SRVR, checked over one run per group, then GC03SRVR
3. Collector reads the state files (6.4), released and deployed
4. Stale device cleanup (6.5) and the dashboard (6.6)
