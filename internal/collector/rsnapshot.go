package collector

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/ha"
	"github.com/giovi321/server-status/internal/model"
)

// rsnapTarget is one monitored rsnapshot config file.
type rsnapTarget struct {
	Path   string
	Name   string
	MaxAge map[string]time.Duration
}

// Rsnapshot monitors rsnapshot backup jobs, one sub-device per config file.
// All state comes from file reads; it never executes rsnapshot.
type Rsnapshot struct {
	cfg        config.Config
	stuckAfter time.Duration
	margin     time.Duration
}

func NewRsnapshot(cfg config.Config) *Rsnapshot {
	return &Rsnapshot{
		cfg:        cfg,
		stuckAfter: rsnapDuration(cfg.Rsnapshot.StuckAfter, 12*time.Hour),
		margin:     rsnapDuration(cfg.Rsnapshot.Margin, 8*time.Hour),
	}
}

// rsnapDuration parses a duration, falling back on empty or invalid input.
func rsnapDuration(s string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

// rsnapMaxAges parses per-interval max_age overrides, dropping invalid values.
func rsnapMaxAges(raw map[string]string) map[string]time.Duration {
	var out map[string]time.Duration
	for k, v := range raw {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			if out == nil {
				out = make(map[string]time.Duration, len(raw))
			}
			out[k] = d
		}
	}
	return out
}

// rsnapExcluded reports whether a discovered conf path is a packaging or editor leftover.
func rsnapExcluded(path string) bool {
	base := filepath.Base(path)
	for _, suffix := range []string{".dpkg-dist", ".dpkg-old", "~", ".bak"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

// rsnapConfName derives the sub-device name: "main" for /etc/rsnapshot.conf,
// otherwise the filename minus the "rsnapshot-" prefix and ".conf" suffix.
func rsnapConfName(path string) string {
	base := filepath.Base(path)
	if base == "rsnapshot.conf" {
		return "main"
	}
	return strings.TrimPrefix(strings.TrimSuffix(base, ".conf"), "rsnapshot-")
}

// rsnapComponent derives the HA sub-device component id; the name is slugged
// because it feeds discovery object_ids, which HA restricts to [a-z0-9_-].
func rsnapComponent(name string) string {
	return "rsnapshot-" + ha.InstanceSlug(name)
}

// rsnapUniqueName resolves sub-device name collisions (compared after
// slugging, since the slug is what HA topics are built from): a duplicate
// falls back to the conf basename, then to numbered suffixes.
func rsnapUniqueName(name, path string, seen map[string]bool) string {
	if !seen[ha.InstanceSlug(name)] {
		seen[ha.InstanceSlug(name)] = true
		return name
	}
	alt := strings.TrimSuffix(filepath.Base(path), ".conf")
	for i := 1; ; i++ {
		if i > 1 {
			alt = strings.TrimSuffix(filepath.Base(path), ".conf") + "-" + strconv.Itoa(i)
		}
		if !seen[ha.InstanceSlug(alt)] {
			seen[ha.InstanceSlug(alt)] = true
			return alt
		}
	}
}

// discoverTargets returns the configs to monitor: the explicit cfg list when
// set, otherwise /etc/rsnapshot.conf plus /etc/rsnapshot-*.conf.
func (r *Rsnapshot) discoverTargets() []rsnapTarget {
	seen := map[string]bool{}
	if len(r.cfg.Rsnapshot.Configs) > 0 {
		var out []rsnapTarget
		for _, e := range r.cfg.Rsnapshot.Configs {
			if e.Path == "" {
				continue
			}
			name := e.Name
			if name == "" {
				name = rsnapConfName(e.Path)
			}
			name = rsnapUniqueName(name, e.Path, seen)
			out = append(out, rsnapTarget{Path: e.Path, Name: name, MaxAge: rsnapMaxAges(e.MaxAge)})
		}
		return out
	}
	var out []rsnapTarget
	if _, err := os.Stat("/etc/rsnapshot.conf"); err == nil {
		out = append(out, rsnapTarget{Path: "/etc/rsnapshot.conf", Name: rsnapUniqueName("main", "/etc/rsnapshot.conf", seen)})
	}
	globbed, _ := filepath.Glob("/etc/rsnapshot-*.conf")
	for _, p := range globbed {
		if rsnapExcluded(p) {
			continue
		}
		out = append(out, rsnapTarget{Path: p, Name: rsnapUniqueName(rsnapConfName(p), p, seen)})
	}
	return out
}

func (*Rsnapshot) Name() string { return "rsnapshot" }

func (r *Rsnapshot) Available() bool {
	for _, t := range r.discoverTargets() {
		if _, err := os.Stat(t.Path); err == nil {
			return true
		}
	}
	return false
}

func (r *Rsnapshot) Collect(ctx context.Context) ([]model.Metric, error) {
	targets := r.discoverTargets()
	out := []model.Metric{
		{Key: "rsnapshot_configs", Name: "Rsnapshot configs", Value: len(targets), StateClass: "measurement", Kind: model.KindSensor, Category: "diagnostic", Icon: "mdi:backup-restore"},
	}
	entries, cronReadable := readCronEntries()
	timers, timerReadable := readTimerUnits()
	paths := make([]string, len(targets))
	for i, t := range targets {
		paths[i] = t.Path
	}
	now := time.Now()
	for _, t := range targets {
		others := make([]string, 0, len(paths))
		for _, p := range paths {
			if p != t.Path {
				others = append(others, p)
			}
		}
		in := r.gatherRsnapshot(t, others, entries, cronReadable, timers, timerReadable)
		st := evaluateRsnapshot(in, now)
		comp := rsnapComponent(t.Name)
		compName := "Rsnapshot " + t.Name
		out = append(out, rsnapshotMetrics(comp, compName, st)...)
		out = append(out, model.Metric{Key: "rsnapshot_stray_items", Component: comp, ComponentName: compName, Name: "Stray items", Value: in.StrayCount, StateClass: "measurement", Kind: model.KindSensor, Category: "diagnostic", Icon: "mdi:file-alert-outline"})
	}
	return out, nil
}

// rsnapLogTailBytes bounds the log read; only the last run's banners matter.
const rsnapLogTailBytes = 256 * 1024

// gatherRsnapshot reads the on-host evidence for one config: conf, log tail,
// lockfile, interval directories, mount state, strays, and cron attribution.
func (r *Rsnapshot) gatherRsnapshot(t rsnapTarget, others []string, cron []cronEntry, cronReadable bool, timers []timerUnit, timerReadable bool) rsnapEvalInput {
	in := rsnapEvalInput{MaxAge: t.MaxAge, Margin: r.margin, StuckAfter: r.stuckAfter, CronReadable: cronReadable, TimerReadable: timerReadable}
	data, err := os.ReadFile(t.Path)
	in.ConfReadable = err == nil
	if err == nil {
		in.Conf = parseRsnapshotConf(string(data))
	}
	in.Conf.Path = t.Path
	// rsnapshot has no built-in lockfile/logfile defaults: unset means no
	// lock is taken and no log is written, so nothing must be read for them
	// (a guessed path would attribute another config's evidence to this one).
	if in.Conf.Logfile != "" {
		if tail, mtime, ok := readFileTail(in.Conf.Logfile, rsnapLogTailBytes); ok {
			in.LogReadable = true
			in.Log = classifyRsnapshotLog(tail)
			in.LogMtime = mtime
		}
	} else {
		in.LogReadable = true // no logfile configured: no log evidence expected
	}
	if in.Conf.Lockfile != "" {
		in.Lock = readRsnapLock(in.Conf.Lockfile)
	}
	if in.Conf.SnapshotRoot != "" {
		in.RootExists, in.RootReadOnly = rsnapRootState(in.Conf.SnapshotRoot)
		in.IntervalDirs = map[string]rsnapDirTimes{}
		for _, iv := range in.Conf.Intervals {
			if dt, ok := rsnapDirStat(filepath.Join(in.Conf.SnapshotRoot, iv.Name+".0")); ok {
				in.IntervalDirs[iv.Name] = dt
			}
		}
		if !(in.Lock.PidAlive && in.Lock.CmdlineMatch) {
			in.StrayCount = rsnapStrayCount(in.Conf.SnapshotRoot, in.Conf.SyncFirst)
		}
	}
	names := make([]string, 0, len(in.Conf.Intervals))
	for _, iv := range in.Conf.Intervals {
		names = append(names, iv.Name)
	}
	in.CronMatches = matchRsnapshotCron(cron, t.Path, t.Path == "/etc/rsnapshot.conf", others, names)
	in.TimerMatches = matchRsnapshotTimers(timers, t.Path, t.Path == "/etc/rsnapshot.conf", names)
	return in
}

// readFileTail returns up to max trailing bytes of a file plus its mtime.
func readFileTail(path string, max int64) (string, time.Time, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", time.Time{}, false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", time.Time{}, false
	}
	if size := fi.Size(); size > max {
		if _, err := f.Seek(size-max, io.SeekStart); err != nil {
			return "", time.Time{}, false
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return "", time.Time{}, false
	}
	return string(data), fi.ModTime(), true
}

// readRsnapLock inspects the lockfile: pid, /proc liveness, and a cmdline
// check that the pid still runs rsnapshot (pid-reuse guard).
func readRsnapLock(path string) rsnapLockState {
	var ls rsnapLockState
	fi, err := os.Stat(path)
	if err != nil {
		return ls
	}
	ls.Exists = true
	ls.Mtime = fi.ModTime()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return rsnapLockState{} // unlinked between stat and read: idle
		}
		return ls // unreadable content counts as unparseable
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		ls.Empty = true
		return ls
	}
	pid, err := strconv.Atoi(s)
	if err != nil || pid <= 0 {
		return ls
	}
	ls.Pid = pid
	proc := "/proc/" + strconv.Itoa(pid)
	if _, err := os.Stat(proc); err != nil {
		return ls
	}
	ls.PidAlive = true
	cmdline, err := os.ReadFile(proc + "/cmdline")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			ls.PidAlive = false // exited between the /proc stat and the read
		}
		return ls
	}
	for _, arg := range strings.Split(string(cmdline), "\x00") {
		if strings.Contains(arg, "rsnapshot") { // also matches rsnapshot_run
			ls.CmdlineMatch = true
			break
		}
	}
	return ls
}

// rsnapDirStat returns mtime and ctime of a path via stat(2).
func rsnapDirStat(path string) (rsnapDirTimes, bool) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return rsnapDirTimes{}, false
	}
	return rsnapDirTimes{
		Mtime: time.Unix(st.Mtim.Unix()),
		Ctime: time.Unix(st.Ctim.Unix()),
	}, true
}

// rsnapRootState reports whether the snapshot root is a readable directory and
// whether its filesystem is mounted read-only.
func rsnapRootState(root string) (exists, readOnly bool) {
	fi, err := os.Stat(root)
	if err != nil || !fi.IsDir() {
		return false, false
	}
	f, err := os.Open(root)
	if err != nil {
		return false, false
	}
	f.Close()
	var st unix.Statfs_t
	if err := unix.Statfs(root, &st); err == nil {
		readOnly = st.Flags&unix.ST_RDONLY != 0
	}
	return true, readOnly
}

// rsnapStrayCount counts leftover work directories in the snapshot root:
// _delete.* always, *.sync only when sync_first is off. Callers skip the scan
// while a live rsnapshot lock is held.
func rsnapStrayCount(root string, syncFirst bool) int {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "_delete.") || (!syncFirst && strings.HasSuffix(name, ".sync")) {
			n++
		}
	}
	return n
}

// readCronEntries reads every cron source rsnapshot jobs realistically live in:
// the root user crontab (both spool layouts), /etc/crontab, and /etc/cron.d.
// readable means at least one source file could be read; absent files are normal.
func readCronEntries() ([]cronEntry, bool) {
	var entries []cronEntry
	readable := false
	for _, p := range []string{"/var/spool/cron/crontabs/root", "/var/spool/cron/root"} {
		if data, err := os.ReadFile(p); err == nil {
			readable = true
			entries = append(entries, parseCronFile(string(data), false, p)...)
		}
	}
	if data, err := os.ReadFile("/etc/crontab"); err == nil {
		readable = true
		entries = append(entries, parseCronFile(string(data), true, "/etc/crontab")...)
	}
	if dents, err := os.ReadDir("/etc/cron.d"); err == nil {
		for _, d := range dents {
			// cron ignores /etc/cron.d names outside [A-Za-z0-9_-]; a dot
			// covers the common leftovers (.dpkg-dist, .bak, ...).
			if d.IsDir() || strings.Contains(d.Name(), ".") {
				continue
			}
			p := "/etc/cron.d/" + d.Name()
			if data, err := os.ReadFile(p); err == nil {
				readable = true
				entries = append(entries, parseCronFile(string(data), true, p)...)
			}
		}
	}
	return entries, readable
}

// rsnapshotMetrics converts one evaluated status into the sub-device's entities.
// rsnapshot_stray_items is emitted by Collect, which holds the eval input's stray count.
func rsnapshotMetrics(component, componentName string, st rsnapStatus) []model.Metric {
	problem := func(key, leaf string, val bool, category string) model.Metric {
		return model.Metric{Key: key, Component: component, ComponentName: componentName, Name: leaf, Value: val, DeviceClass: "problem", Kind: model.KindBinarySensor, Category: category}
	}
	text := func(key, leaf, val, category, icon string) model.Metric {
		return model.Metric{Key: key, Component: component, ComponentName: componentName, Name: leaf, Value: val, Kind: model.KindText, Category: category, Icon: icon}
	}
	out := []model.Metric{
		problem("rsnapshot_problem", "Problem", st.Problem, "primary"),
		text("rsnapshot_state", "State", st.State, "primary", "mdi:backup-restore"),
		text("rsnapshot_last_result", "Last result", st.LastResult, "primary", "mdi:history"),
	}
	if !st.LastSuccess.IsZero() {
		out = append(out, model.Metric{Key: "rsnapshot_last_success", Component: component, ComponentName: componentName, Name: "Last success", Value: st.LastSuccess.UTC().Format(time.RFC3339), DeviceClass: "timestamp", Kind: model.KindSensor, Category: "primary"})
	}
	if st.StaleKnown {
		out = append(out, problem("rsnapshot_stale", "Stale", st.Stale, "primary"))
	}
	out = append(out, problem("rsnapshot_stuck", "Stuck", st.Stuck, "primary"))
	intervals := make([]string, 0, len(st.IntervalAges))
	for iv := range st.IntervalAges {
		intervals = append(intervals, iv)
	}
	sort.Strings(intervals)
	// Instance feeds HA topics via InstanceSlug; names that collide after
	// slugging ("Daily" vs "daily") get a stable ordinal suffix.
	seenSlugs := map[string]bool{}
	for _, iv := range intervals {
		inst := iv
		slug := ha.InstanceSlug(inst)
		for i := 2; seenSlugs[slug]; i++ {
			inst = iv + "-" + strconv.Itoa(i)
			slug = ha.InstanceSlug(inst)
		}
		seenSlugs[slug] = true
		out = append(out, model.Metric{Key: "rsnapshot_interval_age", Component: component, ComponentName: componentName, Instance: inst, Name: iv + " age", Value: st.IntervalAges[iv], Unit: "h", StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:history"})
	}
	return append(out,
		model.Metric{Key: "rsnapshot_running", Component: component, ComponentName: componentName, Name: "Running", Value: st.Running, DeviceClass: "running", Kind: model.KindBinarySensor, Category: "diagnostic"},
		problem("rsnapshot_stale_lock", "Stale lock", st.StaleLock, "diagnostic"),
		problem("rsnapshot_root_missing", "Root missing", st.RootMissing, "diagnostic"),
		problem("rsnapshot_root_readonly", "Root read-only", st.RootReadOnly, "diagnostic"),
		problem("rsnapshot_config_error", "Config error", st.ConfigError, "diagnostic"),
		model.Metric{Key: "rsnapshot_cron_jobs", Component: component, ComponentName: componentName, Name: "Cron jobs", Value: st.CronJobs, StateClass: "measurement", Kind: model.KindSensor, Category: "diagnostic", Icon: "mdi:calendar-clock"},
		text("rsnapshot_cron_list", "Cron list", st.CronList, "diagnostic", "mdi:calendar-clock"),
		model.Metric{Key: "rsnapshot_timer_jobs", Component: component, ComponentName: componentName, Name: "Timer jobs", Value: st.TimerJobs, StateClass: "measurement", Kind: model.KindSensor, Category: "diagnostic", Icon: "mdi:timer-outline"},
		text("rsnapshot_timer_list", "Timer list", st.TimerList, "diagnostic", "mdi:timer-outline"),
		text("rsnapshot_intervals", "Intervals", st.IntervalsText, "diagnostic", "mdi:layers-triple-outline"),
		text("rsnapshot_details", "Details", st.Details, "diagnostic", "mdi:information-outline"),
	)
}
