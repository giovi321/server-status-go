# rsnapshot status Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Each rsnapshot config's Home Assistant device reports only its own runs, plus per-run duration, exit code, data transferred and logical size, shown on a Home Assistant backups view.

**Architecture:** The server-status-go collector filters the shared rsnapshot log by `-c <config>` and parses ISO timestamps. The server-scripts wrapper `rsnapshot_run.sh` captures rsync `--stats` and writes per-run JSON state files to `/var/lib/rsnapshot-status/`, which the collector publishes as new entities. Home Assistant gets a view inside the captured `dashboard_admin` dashboard.

**Tech Stack:** Go (server-status-go, `go test ./...`), bash (server-scripts, `lint.sh`, `scripts-deployer.sh`), rsnapshot 1.5.1, Home Assistant MQTT discovery.

**Spec:** `docs/superpowers/specs/2026-10-31-rsnapshot-status-design.md` (server-status-go)

## Global Constraints

- Nothing may walk snapshot trees or landing folders (spec section 7)
- No alerting changes: the webhook call in the wrapper stays exactly as it is
- Writing state files must never change the wrapper's exit code
- `rsync_long_args` value: `--delete --numeric-ids --relative --delete-excluded --stats`
- State dir `/var/lib/rsnapshot-status/`; files `<conf basename without .conf>.<interval>.json` and `<conf basename without .conf>.last-sync.json`
- `last-sync.json` is written only for a synced run with exit code 0 or 2
- Collector must keep working on hosts with no state dir and on single-config hosts
- Commits attributed to giovi321 only, no AI trailers; branch off `origin/main`, PR, squash-merge
- GC03SRVR changes reach the host as a command Giovanni runs; GC01SRVR is deployed directly
- Home Assistant text states stay under 255 characters (`rsnapClip`)

## Review Focus

- A config with no block in the log tail and no state files (group C before its first 02:10 run): Last result `unknown`, no run entities, no panic. Test in Task 1 and Task 3
- A tail mixing old `[10/Jul/2026:05:20:01]` and ISO `[2026-10-31T04:19:37]` lines (hosts that upgraded rsnapshot): both parse, last block chosen by order in the file. Test in Task 1
- A malformed or half-written state file: ignored, every other entity still published. Test in Task 3
- rsync numbers with thousands separators (`Total file size: 1,234,567 bytes`) and a run where no stats block was parsed: bytes exact; stats fields absent. Test in Task 2
- A run killed by `timeout` (exit 124): per-interval file records 124, `last-sync.json` untouched. Test in Task 2

---

### Task 1: Per-config log attribution and ISO timestamps (server-status-go)

**Files:**
- Modify: `internal/collector/rsnapshotlog.go`
- Modify: `internal/collector/rsnapshot.go` (`gatherRsnapshot`, `rsnapLogTailBytes`)
- Test: `internal/collector/rsnapshotlog_test.go`, `internal/collector/rsnapshot_test.go`

**Interfaces:**
- Produces: `classifyRsnapshotLog(tail, confPath string) rsnapLogState`; `rsnapLogState` gains `LastActivity time.Time` (newest timestamp of any line inside this config's blocks); `rsnapBannerConf(content string) string`

- [ ] **Step 1: Write the failing tests** in `rsnapshotlog_test.go`. Add a fixture `rsnapLogShared` copied from the real GC03SRVR log shape (ISO timestamps, groups a, b and c, `-c /etc/rsnapshot-<g>.conf` banners): c `daysago` started 02:10:00 with an `ERROR:` line and `completed, but with some errors` at 02:12:00, then a `hoursago` started 04:00:17 and `completed successfully` 04:19:37.

```go
func TestClassifyRsnapshotLogPerConfig(t *testing.T) {
	c := classifyRsnapshotLog(rsnapLogShared, "/etc/rsnapshot-c.conf")
	if c.LastResult != "errors" || c.LastInterval != "daysago" { t.Fatalf("c: %+v", c) }
	a := classifyRsnapshotLog(rsnapLogShared, "/etc/rsnapshot-a.conf")
	if a.LastResult != "success" || a.LastInterval != "hoursago" { t.Fatalf("a: %+v", a) }
	if want := time.Date(2026, 10, 31, 4, 0, 17, 0, time.Local); !a.LastStart.Equal(want) { t.Fatalf("a start %v", a.LastStart) }
	if want := time.Date(2026, 10, 31, 2, 12, 0, 0, time.Local); !c.LastActivity.Equal(want) { t.Fatalf("c activity %v", c.LastActivity) }
	if b := classifyRsnapshotLog(rsnapLogShared, "/etc/rsnapshot-b.conf"); b.LastResult != "unknown" { t.Fatalf("b: %+v", b) }
}
```

Also: `TestClassifyRsnapshotLogNoConfFlag` (a banner `/usr/bin/rsnapshot hoursago: started` counts for `/etc/rsnapshot.conf` and is `unknown` for `/etc/rsnapshot-a.conf`), `TestClassifyRsnapshotLogMixedFormats` (an old-format block followed by an ISO block for the same config: the ISO block wins, both start times parsed). Existing tests pass `"/etc/rsnapshot.conf"` as the new argument and keep their expectations.

- [ ] **Step 2: Run** `go test ./internal/collector/ -run 'ClassifyRsnapshotLog'`. Expected: FAIL (compile error on the new argument)

- [ ] **Step 3: Implement.** `rsnapLogLine` tries `"2006-01-02T15:04:05"` then `"02/Jan/2006:15:04:05"`, both with `time.ParseInLocation(..., time.Local)`. `rsnapBannerConf` returns the token after `-c`, or `/etc/rsnapshot.conf` when there is none. Every block records its conf. Completion banners close the newest open block with the same conf and interval. `ERROR`/`WARNING` and other lines go to the newest open block of any conf (runs are serialised by the shared flock) and advance that block's last-line time. The result is built from the newest block whose conf equals `confPath`; `LastActivity` is that block's last line time.

- [ ] **Step 4: Gather.** In `gatherRsnapshot`, call `classifyRsnapshotLog(tail, t.Path)` and set `in.LogMtime = in.Log.LastActivity` instead of the file mtime, so another group writing the log cannot keep this config's dead-man check fresh. Raise `rsnapLogTailBytes` to `1 << 20`. Add `TestGatherRsnapshotSharedLog` in `rsnapshot_test.go`: a temp conf for group c plus a temp log holding `rsnapLogShared`, run `gatherRsnapshot`, and assert `in.Log.LastResult == "errors"` and `in.LogMtime` equal to 02:12:00.

- [ ] **Step 5: Run** `go test ./...` and `go vet ./...`. Expected: PASS, no vet output

- [ ] **Step 6: Commit, PR, squash-merge** (`Attribute shared rsnapshot log runs to their own config`), then tag the next patch release (`git tag v1.0.6 && git push origin v1.0.6`) and confirm the release workflow publishes binaries

- [ ] **Step 7: Deploy to GC01SRVR** by re-running the README's install command with `--non-interactive` on the host (it keeps `/etc/server-status/config.yaml`), then confirm `systemctl is-active server-status` and that `server-status --version` (or the HA version entity) reads `v1.0.6`. For GC03SRVR, hand Giovanni the same command wrapped in the usual `ssh root@192.168.1.10 'ssh -i /root/.ssh/gc03srvr-rsync root@10.10.10.30 ...'`. Check in HA that "Rsnapshot c" on GC03SRVR no longer shows group A's result

### Task 2: Wrapper state files and `--stats` (server-scripts)

**Files:**
- Create: `common/rsnapshot_state.sh`
- Create: `common/tests/rsnapshot_state_test.sh` (plus fixture `common/tests/fixtures/rsnapshot-stats.out`)
- Modify: `GC01SRVR/rsnapshot_run.sh`, `GC03SRVR/rsnapshot_run.sh`
- Modify: `GC01SRVR/rsnapshot-{a,b,c}.conf`, `GC03SRVR/rsnapshot-{a,b,c,vm,gc01srvr}.conf`
- Modify: `GC01SRVR/deploy.manifest`, `GC03SRVR/deploy.manifest` (deploy the new common script the way `common/rsnapshot_log_webhook.sh` is deployed to these two hosts, with a `bash -n` run-after like `failed-units-check.sh`'s)

**Interfaces:**
- Produces: `rsnapshot_state.sh <conf> <interval> <rc> <start-epoch> <end-epoch> <stdout-file> [state-dir]`, default state dir `/var/lib/rsnapshot-status`. JSON fields exactly as spec 6.3: `config`, `interval`, `start`, `end` (RFC 3339 with zone), `exit_code`, `synced`, `bytes_transferred`, `bytes_total`, `files_transferred`, `backup_points`. Byte and file counts are JSON integers
- `synced` is true when `<interval>` equals the config's first `retain`/`interval` line

- [ ] **Step 1: Write the failing test** `common/tests/rsnapshot_state_test.sh`: plain bash, `set -eu`, a temp state dir, a temp conf whose first `retain` is `hoursago`. Cases:
  - fixture with two rsync `--stats` blocks (`Number of regular files transferred: 1,203`, `Total file size: 1,234,567 bytes`, `Total transferred file size: 98,765 bytes`, and a second block 10 / 1,000 / 100), rc 0, interval hoursago: `rsnapshot-x.hoursago.json` has `"bytes_total":1235567`, `"bytes_transferred":98865`, `"files_transferred":1213`, `"backup_points":2`, `"synced":true`; `rsnapshot-x.last-sync.json` exists with the same values
  - same fixture, rc 124: per-interval file has `"exit_code":124`; `last-sync.json` from the previous case is byte-identical
  - rc 2: `last-sync.json` rewritten
  - empty stdout, rc 0: no stats keys in the JSON, `last-sync.json` not written
  - interval `daysago`: `"synced":false`, no stats keys, `last-sync.json` untouched
  - each output parses with `python3 -m json.tool`

- [ ] **Step 2: Run** `bash common/tests/rsnapshot_state_test.sh`. Expected: FAIL (script missing)

- [ ] **Step 3: Implement** `common/rsnapshot_state.sh`: awk sums the three stats lines with commas stripped; temp file in the state dir then `mv`; `mkdir -p` the dir with mode 755, files 644. Exit 0 on its own failures after printing to stderr.

- [ ] **Step 4: Run the test.** Expected: PASS

- [ ] **Step 5: Wire the wrappers.** In both `rsnapshot_run.sh`: record `start=$(date +%s)`, run rsnapshot as now but `| tee "$out"` into a `mktemp` file so stdout still reaches the journal, take `rc=${PIPESTATUS[0]}`, record `end`, call `/home/programmi/rsnapshot_state.sh "$CONF" "$INTERVAL" "$rc" "$start" "$end" "$out" || true`, remove the temp file, then the unchanged webhook call and `exit "$rc"`.

- [ ] **Step 6: Configs.** Add `rsync_long_args<TAB>--delete --numeric-ids --relative --delete-excluded --stats` (real tab) to the eight configs, next to the existing `cmd_*` lines.

- [ ] **Step 7: Run** `./lint.sh`. Expected: clean

- [ ] **Step 8: Commit, PR, squash-merge** (`Record per-run rsnapshot state with rsync stats`)

- [ ] **Step 9: Deploy to GC01SRVR** with `git pull` and `./scripts-deployer.sh --all GC01SRVR` in `/home/programmi/server-scripts`. Expected: `configtest` passes for a, b and c. After the next a and b `hoursago` runs, check that `/var/lib/rsnapshot-status/` holds `rsnapshot-a.hoursago.json`, `rsnapshot-a.last-sync.json` and the b pair with non-zero `bytes_total`, and that the rsync command lines in `/var/log/rsnapshot.log` end in `--stats`. After C's 02:10 run, check its pair too

- [ ] **Step 10: GC03SRVR.** Once GC01SRVR has a clean day, hand Giovanni one command (git pull plus `./scripts-deployer.sh --all GC03SRVR` in `/home/programmi/server-scripts`, through the GC01SRVR jump). Verify the same files for a, b, c, vm and gc01srvr after their runs

### Task 3: Collector publishes the run state (server-status-go)

**Files:**
- Create: `internal/collector/rsnapshotrun.go`
- Test: `internal/collector/rsnapshotrun_test.go`
- Modify: `internal/collector/rsnapshot.go` (`Rsnapshot` gains `stateDir string`, default `"/var/lib/rsnapshot-status"`; `Collect` appends the run metrics per config)

**Interfaces:**
- Consumes: Task 2's JSON layout; Task 1's `rsnapLogState.LastEnd`
- Produces:
  - `type rsnapRun struct` with JSON-tagged fields `Config`, `Interval`, `Start`, `End time.Time`, `ExitCode int`, `Synced bool`, `BytesTransferred`, `BytesTotal`, `FilesTransferred *int64`, `BackupPoints int`
  - `readRsnapRuns(dir, name string) (last, lastSync *rsnapRun)`: `name` is the conf basename without `.conf`; `last` is the newest `End` among `<name>.*.json` excluding `last-sync`; unreadable or invalid files are skipped
  - `rsnapshotRunMetrics(component, componentName string, last, lastSync *rsnapRun, log rsnapLogState) []model.Metric`

- [ ] **Step 1: Write the failing tests** in `rsnapshotrun_test.go`:
  - `TestReadRsnapRuns`: temp dir with `rsnapshot-a.hoursago.json` (end 08:01), `rsnapshot-a.daysago.json` (end 02:00), `rsnapshot-a.last-sync.json`, `rsnapshot-b.hoursago.json` (end 09:01), and a truncated `rsnapshot-a.weeksago.json`. Expect `last.Interval == "hoursago"` for `rsnapshot-a`, `lastSync` non-nil, b's file not read
  - `TestReadRsnapRunsMissingDir`: both nil, no error
  - `TestRsnapshotRunMetrics`: keys and values for `rsnapshot_last_run` (timestamp, RFC 3339 UTC), `rsnapshot_last_run_interval` (text), `rsnapshot_last_run_duration` (`Unit: "s"`, `DeviceClass: "duration"`), `rsnapshot_last_exit_code`, `rsnapshot_last_sync` (timestamp), `rsnapshot_last_sync_transferred` and `rsnapshot_last_sync_size` (`Unit: "B"`, `DeviceClass: "data_size"`, `StateClass: "measurement"`), `rsnapshot_last_sync_files` (`StateClass: "measurement"`)
  - `TestRsnapshotRunMetricsLogNewer`: `log.LastEnd` after `last.End` gives `rsnapshot_last_run` and interval from the log and no `rsnapshot_last_exit_code` and no duration
  - nil `last` and nil `lastSync`: no metrics returned

- [ ] **Step 2: Run** `go test ./internal/collector/ -run 'RsnapRuns|RunMetrics'`. Expected: FAIL (undefined)

- [ ] **Step 3: Implement** `readRsnapRuns` and `rsnapshotRunMetrics` in `rsnapshotrun.go`; in `Collect`, read with `strings.TrimSuffix(filepath.Base(t.Path), ".conf")` and append the metrics to the config's existing component. All new entities use `Category: "primary"` except exit code and files, which are `"diagnostic"`

- [ ] **Step 4: Run** `go test ./...` and `go vet ./...`. Expected: PASS

- [ ] **Step 5: Docs.** Add the new entities and the state file contract to `docs/src/content/docs/metrics/rsnapshot.md` under Entities

- [ ] **Step 6: Commit, PR, squash-merge** (`Publish rsnapshot run duration, exit code and sync size`), tag `v1.1.0`, deploy as in Task 1 Step 7. Check the new entities appear on every config's device on GC01SRVR, then GC03SRVR

### Task 4: Stale devices and the backups view (Home Assistant)

**Files:**
- Modify: Home Assistant `dashboard_admin` dashboard (new "Backups" view), captured by `GC01haSRVR/capture.manifest` into server-scripts on the next nightly run

- [ ] **Step 1: Remove** the "Rsnapshot main" device on GC01SRVR and GC03SRVR (MQTT device page, Delete), and confirm it does not return after one server-status publish cycle

- [ ] **Step 2: Build the view.** One section per host; per config one card with: Problem/State (colour by state), Last success, Last run, Last run duration, Last exit code, Last sync transferred, Last sync size, and each interval age. Below: history graphs over 30 days for transferred, size and duration per config, and the `/media/backup` (GC01SRVR) and `/media/storage1` (GC03SRVR) filesystem usage entities. Core cards first; a custom card only where a core card cannot show it

- [ ] **Step 3: Verify** the next morning that the nightly capture committed the view to server-scripts, then add one line to Appendix C #216 and the server-status-go entry in the homelab docs saying where the view lives
