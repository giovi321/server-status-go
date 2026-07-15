package collector

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/ha"
	"github.com/giovi321/server-status/internal/model"
)

func TestRsnapshotMetricsAssembly(t *testing.T) {
	st := rsnapStatus{
		State:       "ok",
		Problem:     false,
		LastResult:  "success",
		LastSuccess: time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC),
		StaleKnown:  true,
		IntervalAges: map[string]float64{
			"hoursago": 3.0, "daysago": 7.2, "weeksago": 79.0, "monthsago": 223.5,
		},
		CronJobs:      4,
		CronList:      "hoursago 0 */6 * * *; daysago 20 5 * * *",
		IntervalsText: "hoursago:6 daysago:7 weeksago:4 monthsago:4",
		Details:       "mount:ok rw:ok conf:ok cron:4 lock:idle stray:0",
	}
	ms := rsnapshotMetrics("rsnapshot-main", "Rsnapshot main", st)
	by := map[string]model.Metric{}
	ages := map[string]model.Metric{}
	for _, m := range ms {
		if m.Component != "rsnapshot-main" || m.ComponentName != "Rsnapshot main" {
			t.Fatalf("sub-device on %s: %+v", m.Key, m)
		}
		if m.Key == "rsnapshot_interval_age" {
			ages[m.Instance] = m
			continue
		}
		by[m.Key] = m
	}
	binaries := map[string]bool{
		"rsnapshot_problem": false, "rsnapshot_stale": false, "rsnapshot_stuck": false,
		"rsnapshot_running": false, "rsnapshot_stale_lock": false, "rsnapshot_root_missing": false,
		"rsnapshot_root_readonly": false, "rsnapshot_config_error": false,
	}
	for key, want := range binaries {
		m, ok := by[key]
		if !ok || m.Kind != model.KindBinarySensor || m.Value != want {
			t.Fatalf("%s: %+v ok=%v", key, m, ok)
		}
	}
	if by["rsnapshot_running"].DeviceClass != "running" || by["rsnapshot_problem"].DeviceClass != "problem" {
		t.Fatalf("device classes: %+v %+v", by["rsnapshot_running"], by["rsnapshot_problem"])
	}
	for key, want := range map[string]string{
		"rsnapshot_state":       "ok",
		"rsnapshot_last_result": "success",
		"rsnapshot_cron_list":   st.CronList,
		"rsnapshot_intervals":   st.IntervalsText,
		"rsnapshot_details":     st.Details,
	} {
		m, ok := by[key]
		if !ok || m.Kind != model.KindText || m.Value != want {
			t.Fatalf("%s: %+v ok=%v", key, m, ok)
		}
	}
	if m := by["rsnapshot_last_success"]; m.Kind != model.KindSensor || m.DeviceClass != "timestamp" || m.Value != "2026-07-10T09:00:00Z" {
		t.Fatalf("last_success: %+v", m)
	}
	if m := by["rsnapshot_cron_jobs"]; m.Kind != model.KindSensor || m.Value != 4 {
		t.Fatalf("cron_jobs: %+v", m)
	}
	if len(ages) != len(st.IntervalAges) {
		t.Fatalf("interval ages: got %d, want %d (%+v)", len(ages), len(st.IntervalAges), ages)
	}
	for iv, want := range st.IntervalAges {
		m, ok := ages[iv]
		if !ok || m.Kind != model.KindSensor || m.Unit != "h" || m.Value != want {
			t.Fatalf("interval_age[%s]: %+v ok=%v", iv, m, ok)
		}
	}
}

func TestRsnapshotMetricsOmissions(t *testing.T) {
	// StaleKnown false -> no stale sensor; zero LastSuccess -> no timestamp.
	st := rsnapStatus{State: "pending", LastResult: "unknown"}
	for _, m := range rsnapshotMetrics("rsnapshot-x", "Rsnapshot x", st) {
		if m.Key == "rsnapshot_stale" || m.Key == "rsnapshot_last_success" || m.Key == "rsnapshot_interval_age" {
			t.Fatalf("%s must be omitted: %+v", m.Key, m)
		}
	}
}

// Interval names that collide after slugging must not share an HA instance.
func TestRsnapshotMetricsIntervalSlugCollision(t *testing.T) {
	st := rsnapStatus{IntervalAges: map[string]float64{"Daily": 1.0, "daily": 2.0}}
	slugs := map[string]bool{}
	n := 0
	for _, m := range rsnapshotMetrics("rsnapshot-x", "Rsnapshot x", st) {
		if m.Key != "rsnapshot_interval_age" {
			continue
		}
		n++
		slug := ha.InstanceSlug(m.Instance)
		if slugs[slug] {
			t.Fatalf("duplicate instance slug %q", slug)
		}
		slugs[slug] = true
	}
	if n != 2 {
		t.Fatalf("interval_age metrics: %d want 2", n)
	}
}

// Component ids feed HA discovery object_ids and must be slug-safe.
func TestRsnapComponent(t *testing.T) {
	cases := map[string]string{
		"main":               "rsnapshot-main",
		"gc01srvr":           "rsnapshot-gc01srvr",
		"backup.example.com": "rsnapshot-backup-example-com",
		"My Conf":            "rsnapshot-my-conf",
	}
	for name, want := range cases {
		if got := rsnapComponent(name); got != want {
			t.Fatalf("rsnapComponent(%q) = %q, want %q", name, got, want)
		}
	}
}

// Colliding sub-device names must be disambiguated, not silently merged.
func TestDiscoverTargetsDedup(t *testing.T) {
	r := NewRsnapshot(config.Config{Rsnapshot: config.RsnapshotConfig{Configs: []config.RsnapshotEntry{
		{Path: "/etc/rsnapshot.conf"},
		{Path: "/etc/rsnapshot-main.conf"},        // derives "main" too
		{Path: "/root/backup.conf", Name: "Main"}, // collides post-slug
	}}})
	targets := r.discoverTargets()
	if len(targets) != 3 {
		t.Fatalf("targets: %+v", targets)
	}
	want := []string{"main", "rsnapshot-main", "backup"}
	seen := map[string]bool{}
	for i, tg := range targets {
		if tg.Name != want[i] {
			t.Fatalf("target %d name %q, want %q", i, tg.Name, want[i])
		}
		slug := ha.InstanceSlug(tg.Name)
		if seen[slug] {
			t.Fatalf("duplicate component slug %q", slug)
		}
		seen[slug] = true
	}
}

// A conf without lockfile/logfile directives uses none: rsnapshot has no
// built-in defaults, so no foreign log or lock may be read for it.
func TestGatherRsnapshotOmittedLogfileLockfile(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "rsnapshot-other.conf")
	root := filepath.Join(dir, "snap")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	conf := "snapshot_root\t" + root + "\nretain\tdaily\t3\n"
	if err := os.WriteFile(confPath, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRsnapshot(config.Config{})
	in := r.gatherRsnapshot(rsnapTarget{Path: confPath, Name: "other"}, nil, nil, false, nil, false)
	if in.Conf.Logfile != "" || in.Conf.Lockfile != "" {
		t.Fatalf("invented defaults: logfile=%q lockfile=%q", in.Conf.Logfile, in.Conf.Lockfile)
	}
	if !in.LogReadable {
		t.Fatal("no logfile configured must count as no log evidence expected")
	}
	if !in.LogMtime.IsZero() || in.Log.LastResult != "" {
		t.Fatalf("log evidence read from nowhere: %+v", in.Log)
	}
	if in.Lock != (rsnapLockState{}) {
		t.Fatalf("lock evidence read from nowhere: %+v", in.Lock)
	}
}

func TestReadRsnapLock(t *testing.T) {
	dir := t.TempDir()
	if ls := readRsnapLock(filepath.Join(dir, "absent.pid")); ls.Exists {
		t.Fatalf("absent: %+v", ls)
	}
	empty := filepath.Join(dir, "empty.pid")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if ls := readRsnapLock(empty); !ls.Exists || !ls.Empty {
		t.Fatalf("empty: %+v", ls)
	}
	garbage := filepath.Join(dir, "garbage.pid")
	if err := os.WriteFile(garbage, []byte("not-a-pid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ls := readRsnapLock(garbage); !ls.Exists || ls.Empty || ls.Pid != 0 {
		t.Fatalf("garbage: %+v", ls)
	}
	// An unreadable-as-file lock (a directory) is unparseable, not idle.
	if ls := readRsnapLock(dir); !ls.Exists || ls.Pid != 0 {
		t.Fatalf("dir: %+v", ls)
	}
	// The test process itself: alive, but its cmdline is not rsnapshot.
	own := filepath.Join(dir, "own.pid")
	if err := os.WriteFile(own, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	if ls := readRsnapLock(own); !ls.Exists || !ls.PidAlive || ls.CmdlineMatch {
		t.Fatalf("own pid: %+v", ls)
	}
}

func TestRsnapConfName(t *testing.T) {
	cases := map[string]string{
		"/etc/rsnapshot.conf":          "main",
		"/etc/rsnapshot-gc01srvr.conf": "gc01srvr",
		"/etc/rsnapshot-offsite.conf":  "offsite",
		"/opt/backup/rsnapshot.conf":   "main",
		"/etc/custom.conf":             "custom",
	}
	for path, want := range cases {
		if got := rsnapConfName(path); got != want {
			t.Fatalf("rsnapConfName(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestRsnapExcluded(t *testing.T) {
	for _, p := range []string{
		"/etc/rsnapshot-a.conf.dpkg-dist", "/etc/rsnapshot-a.conf.dpkg-old",
		"/etc/rsnapshot-a.conf~", "/etc/rsnapshot-a.conf.bak",
	} {
		if !rsnapExcluded(p) {
			t.Fatalf("%s must be excluded", p)
		}
	}
	if rsnapExcluded("/etc/rsnapshot-gc01srvr.conf") {
		t.Fatal("real conf must not be excluded")
	}
}
