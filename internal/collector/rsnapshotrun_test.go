package collector

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giovi321/server-status/internal/model"
)

func writeRun(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadRsnapRuns(t *testing.T) {
	dir := t.TempDir()
	writeRun(t, dir, "rsnapshot-a.hoursago.json", `{"config":"/etc/rsnapshot-a.conf","interval":"hoursago","start":"2026-10-31T08:00:17+01:00","end":"2026-10-31T08:01:34+01:00","exit_code":0,"synced":true,"files_transferred":1213,"bytes_total":1235567,"bytes_transferred":98865,"backup_points":2}`)
	writeRun(t, dir, "rsnapshot-a.daysago.json", `{"config":"/etc/rsnapshot-a.conf","interval":"daysago","start":"2026-10-31T02:00:00+01:00","end":"2026-10-31T02:00:40+01:00","exit_code":0,"synced":false}`)
	writeRun(t, dir, "rsnapshot-a.last-sync.json", `{"config":"/etc/rsnapshot-a.conf","interval":"hoursago","start":"2026-10-31T04:00:17+01:00","end":"2026-10-31T04:19:37+01:00","exit_code":0,"synced":true,"files_transferred":5,"bytes_total":1000,"bytes_transferred":10,"backup_points":8}`)
	writeRun(t, dir, "rsnapshot-a.weeksago.json", `{"config":"/etc/rsnapshot-a.conf","interval":"weeksago","end":"2026-10-31T23`)
	writeRun(t, dir, "rsnapshot-b.hoursago.json", `{"config":"/etc/rsnapshot-b.conf","interval":"hoursago","start":"2026-10-31T09:00:17+01:00","end":"2026-10-31T09:01:05+01:00","exit_code":0,"synced":true}`)

	last, lastSync := readRsnapRuns(dir, "rsnapshot-a")
	if last == nil || last.Interval != "hoursago" || last.ExitCode != 0 {
		t.Fatalf("last %+v", last)
	}
	if last.BytesTotal == nil || *last.BytesTotal != 1235567 {
		t.Fatalf("last bytes_total %v", last.BytesTotal)
	}
	if lastSync == nil || lastSync.BytesTotal == nil || *lastSync.BytesTotal != 1000 {
		t.Fatalf("lastSync %+v", lastSync)
	}
	if want := time.Date(2026, 10, 31, 8, 1, 34, 0, time.FixedZone("", 3600)); !last.End.Equal(want) {
		t.Fatalf("end %v", last.End)
	}
}

func TestReadRsnapRunsMissingDir(t *testing.T) {
	last, lastSync := readRsnapRuns(filepath.Join(t.TempDir(), "absent"), "rsnapshot-a")
	if last != nil || lastSync != nil {
		t.Fatalf("got %+v %+v", last, lastSync)
	}
}

func runMetric(t *testing.T, ms []model.Metric, key string) (model.Metric, bool) {
	t.Helper()
	for _, m := range ms {
		if m.Key == key {
			return m, true
		}
	}
	return model.Metric{}, false
}

func TestRsnapshotRunMetrics(t *testing.T) {
	start := time.Date(2026, 10, 31, 8, 0, 17, 0, time.UTC)
	last := &rsnapRun{Interval: "hoursago", Start: start, End: start.Add(77 * time.Second), ExitCode: 2, Synced: true}
	sync := &rsnapRun{Interval: "hoursago", Start: start, End: start.Add(77 * time.Second), Synced: true,
		BytesTransferred: i64(98865), BytesTotal: i64(1235567), FilesTransferred: i64(1213)}
	ms := rsnapshotRunMetrics("rsnapshot-a", "Rsnapshot a", last, sync, rsnapLogState{})

	cases := []struct {
		key, unit, devClass, stateClass string
		value                           any
	}{
		{"rsnapshot_last_run", "", "timestamp", "", "2026-10-31T08:01:34Z"},
		{"rsnapshot_last_run_interval", "", "", "", "hoursago"},
		{"rsnapshot_last_run_duration", "s", "duration", "measurement", int64(77)},
		{"rsnapshot_last_exit_code", "", "", "", 2},
		{"rsnapshot_last_sync", "", "timestamp", "", "2026-10-31T08:01:34Z"},
		{"rsnapshot_last_sync_transferred", "B", "data_size", "measurement", int64(98865)},
		{"rsnapshot_last_sync_size", "B", "data_size", "measurement", int64(1235567)},
		{"rsnapshot_last_sync_files", "", "", "measurement", int64(1213)},
	}
	for _, c := range cases {
		m, ok := runMetric(t, ms, c.key)
		if !ok {
			t.Fatalf("missing %s", c.key)
		}
		if m.Component != "rsnapshot-a" || m.ComponentName != "Rsnapshot a" {
			t.Fatalf("%s component %q %q", c.key, m.Component, m.ComponentName)
		}
		if m.Unit != c.unit || m.DeviceClass != c.devClass || m.StateClass != c.stateClass || m.Value != c.value {
			t.Fatalf("%s: got unit=%q class=%q state=%q value=%#v", c.key, m.Unit, m.DeviceClass, m.StateClass, m.Value)
		}
	}
}

// When the log shows a run newer than the state file (the wrapper was
// bypassed, or a run is in progress), the last run time and interval come
// from the log, and nothing from the stale state file is claimed for it.
func TestRsnapshotRunMetricsLogNewer(t *testing.T) {
	start := time.Date(2026, 10, 31, 8, 0, 17, 0, time.UTC)
	last := &rsnapRun{Interval: "hoursago", Start: start, End: start.Add(77 * time.Second), ExitCode: 0, Synced: true}
	log := rsnapLogState{LastInterval: "daysago", LastStart: start.Add(time.Hour), LastEnd: start.Add(time.Hour + time.Minute)}
	ms := rsnapshotRunMetrics("rsnapshot-a", "Rsnapshot a", last, nil, log)
	if m, _ := runMetric(t, ms, "rsnapshot_last_run"); m.Value != "2026-10-31T09:01:17Z" {
		t.Fatalf("last run %#v", m.Value)
	}
	if m, _ := runMetric(t, ms, "rsnapshot_last_run_interval"); m.Value != "daysago" {
		t.Fatalf("interval %#v", m.Value)
	}
	for _, k := range []string{"rsnapshot_last_exit_code", "rsnapshot_last_run_duration"} {
		if _, ok := runMetric(t, ms, k); ok {
			t.Fatalf("%s published from a stale state file", k)
		}
	}
}

// A run still in progress (started, no completion banner) must not take over
// the last run entities: they keep describing the last completed run, so its
// time, exit code and duration stay paired.
func TestRsnapshotRunMetricsRunInProgress(t *testing.T) {
	start := time.Date(2026, 10, 31, 8, 0, 17, 0, time.UTC)
	last := &rsnapRun{Interval: "hoursago", Start: start, End: start.Add(77 * time.Second), ExitCode: 2, Synced: true}
	log := rsnapLogState{LastInterval: "daysago", LastStart: start.Add(time.Hour), LastResult: "incomplete"}
	ms := rsnapshotRunMetrics("rsnapshot-a", "Rsnapshot a", last, nil, log)
	if m, _ := runMetric(t, ms, "rsnapshot_last_run"); m.Value != "2026-10-31T08:01:34Z" {
		t.Fatalf("last run %#v", m.Value)
	}
	if m, ok := runMetric(t, ms, "rsnapshot_last_exit_code"); !ok || m.Value != 2 {
		t.Fatalf("exit code %#v %v", m.Value, ok)
	}
}

func TestRsnapshotRunMetricsNone(t *testing.T) {
	if ms := rsnapshotRunMetrics("rsnapshot-a", "Rsnapshot a", nil, nil, rsnapLogState{}); len(ms) != 0 {
		t.Fatalf("got %d metrics", len(ms))
	}
}
