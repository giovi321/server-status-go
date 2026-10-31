// rsnapshot per-run state files: written by the server-scripts wrapper
// (common/rsnapshot_state.sh) after every run, read here. Sizes come from
// rsync --stats, so nothing in this path walks a snapshot tree.

package collector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/giovi321/server-status/internal/model"
)

// rsnapStateDir is where the wrapper writes the state files.
const rsnapStateDir = "/var/lib/rsnapshot-status"

// rsnapRun is one recorded run: <config>.<interval>.json for every run, and
// <config>.last-sync.json for the last syncing run that exited 0 or 2. The
// stats are nil when the run printed no rsync --stats.
type rsnapRun struct {
	Config           string    `json:"config"`
	Interval         string    `json:"interval"`
	Start            time.Time `json:"start"`
	End              time.Time `json:"end"`
	ExitCode         int       `json:"exit_code"`
	Synced           bool      `json:"synced"`
	BytesTransferred *int64    `json:"bytes_transferred"`
	BytesTotal       *int64    `json:"bytes_total"`
	FilesTransferred *int64    `json:"files_transferred"`
	BackupPoints     int       `json:"backup_points"`
}

// readRsnapRunFile parses one state file; a missing, unreadable or invalid
// file (including one caught half-written) is nil.
func readRsnapRunFile(path string) *rsnapRun {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var r rsnapRun
	if json.Unmarshal(data, &r) != nil || r.End.IsZero() {
		return nil
	}
	return &r
}

// readRsnapRuns returns the newest run recorded for a config (name is the
// conf basename without ".conf") and its last good sync. Both are nil when
// the directory or the files are absent.
func readRsnapRuns(dir, name string) (last, lastSync *rsnapRun) {
	paths, _ := filepath.Glob(filepath.Join(dir, name+".*.json"))
	syncPath := filepath.Join(dir, name+".last-sync.json")
	for _, p := range paths {
		if p == syncPath {
			continue
		}
		// a glob on "rsnapshot-a.*" must not pick up "rsnapshot-a.b.x.json"
		// from a config called rsnapshot-a.b: the interval has no dot
		if strings.Contains(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), name+"."), ".json"), ".") {
			continue
		}
		if r := readRsnapRunFile(p); r != nil && (last == nil || r.End.After(last.End)) {
			last = r
		}
	}
	return last, readRsnapRunFile(syncPath)
}

// rsnapshotRunMetrics publishes the last run and the last good sync on the
// config's device. When the log holds a newer completed run than the state
// file (the wrapper was bypassed, or the state write failed), the last run
// time and interval come from the log and the state file's exit code and
// duration are not claimed for it.
func rsnapshotRunMetrics(component, componentName string, last, lastSync *rsnapRun, log rsnapLogState) []model.Metric {
	var out []model.Metric
	metric := func(key, leaf string, val any, unit, devClass, stateClass, category, icon string, kind model.Kind) {
		out = append(out, model.Metric{Key: key, Component: component, ComponentName: componentName, Name: leaf, Value: val,
			Unit: unit, DeviceClass: devClass, StateClass: stateClass, Kind: kind, Category: category, Icon: icon})
	}
	stamp := func(t time.Time) string { return t.UTC().Format(time.RFC3339) }

	// Only a completed log block can take over: during a run in progress the
	// entities keep describing the last completed run, so its time, exit code
	// and duration stay paired (MQTT keeps a metric that stops being sent).
	logEnd := log.LastEnd
	switch {
	case !logEnd.IsZero() && (last == nil || logEnd.After(last.End)):
		metric("rsnapshot_last_run", "Last run", stamp(logEnd), "", "timestamp", "", "primary", "", model.KindSensor)
		metric("rsnapshot_last_run_interval", "Last run interval", log.LastInterval, "", "", "", "primary", "mdi:layers-outline", model.KindText)
	case last != nil:
		metric("rsnapshot_last_run", "Last run", stamp(last.End), "", "timestamp", "", "primary", "", model.KindSensor)
		metric("rsnapshot_last_run_interval", "Last run interval", last.Interval, "", "", "", "primary", "mdi:layers-outline", model.KindText)
		if !last.Start.IsZero() && !last.End.Before(last.Start) {
			metric("rsnapshot_last_run_duration", "Last run duration", int64(last.End.Sub(last.Start)/time.Second), "s", "duration", "measurement", "primary", "mdi:timer-outline", model.KindSensor)
		}
		metric("rsnapshot_last_exit_code", "Last exit code", last.ExitCode, "", "", "", "diagnostic", "mdi:numeric", model.KindSensor)
	}

	if lastSync != nil {
		metric("rsnapshot_last_sync", "Last good sync", stamp(lastSync.End), "", "timestamp", "", "primary", "", model.KindSensor)
		if lastSync.BytesTransferred != nil {
			metric("rsnapshot_last_sync_transferred", "Last sync transferred", *lastSync.BytesTransferred, "B", "data_size", "measurement", "primary", "mdi:transfer", model.KindSensor)
		}
		if lastSync.BytesTotal != nil {
			metric("rsnapshot_last_sync_size", "Last sync size", *lastSync.BytesTotal, "B", "data_size", "measurement", "primary", "mdi:database", model.KindSensor)
		}
		if lastSync.FilesTransferred != nil {
			metric("rsnapshot_last_sync_files", "Last sync files transferred", *lastSync.FilesTransferred, "", "", "measurement", "diagnostic", "mdi:file-multiple-outline", model.KindSensor)
		}
	}
	return out
}
