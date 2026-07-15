// rsnapshot state evaluation: folds conf, log, lock, cron, and directory
// evidence into one status. Pure; the clock is injected.

package collector

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// rsnapLockState is the observed state of an rsnapshot lockfile.
type rsnapLockState struct {
	Exists       bool
	Empty        bool
	Pid          int
	PidAlive     bool
	CmdlineMatch bool
	Mtime        time.Time
}

// rsnapDirTimes carries the mtime and ctime of one interval directory.
type rsnapDirTimes struct {
	Mtime time.Time
	Ctime time.Time
}

// rsnapEvalInput is everything evaluateRsnapshot needs, gathered by the collector.
type rsnapEvalInput struct {
	Conf          rsnapConf
	ConfReadable  bool
	Log           rsnapLogState
	LogReadable   bool
	LogMtime      time.Time
	Lock          rsnapLockState
	IntervalDirs  map[string]rsnapDirTimes
	CronMatches   map[string][]cronEntry
	CronReadable  bool
	TimerMatches  map[string][]timerUnit
	TimerReadable bool
	RootExists    bool
	RootReadOnly  bool
	StrayCount    int
	MaxAge        map[string]time.Duration
	Margin        time.Duration
	StuckAfter    time.Duration
}

// rsnapStatus is the evaluated status of one rsnapshot config.
type rsnapStatus struct {
	State         string
	Problem       bool
	Reasons       []string
	LastResult    string
	LastSuccess   time.Time
	StaleKnown    bool
	Stale         bool
	Stuck         bool
	StaleLock     bool
	Running       bool
	RootMissing   bool
	RootReadOnly  bool
	ConfigError   bool
	IntervalAges  map[string]float64
	CronJobs      int
	CronList      string
	TimerJobs     int
	TimerList     string
	IntervalsText string
	Details       string
}

// rsnapEmptyLockGrace is how long an empty lockfile stays indeterminate before
// it counts as stale (rsnapshot writes the pid right after creating the file).
const rsnapEmptyLockGrace = 5 * time.Minute

// rsnapTextLimit caps text values; HA drops states longer than 255 chars.
const rsnapTextLimit = 255

// rsnapClip truncates s to rsnapTextLimit with a "..." marker, backing off to
// a rune boundary so the cut never splits a multi-byte character.
func rsnapClip(s string) string {
	if len(s) <= rsnapTextLimit {
		return s
	}
	cut := rsnapTextLimit - 3
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// evaluateRsnapshot folds the gathered evidence into one status.
func evaluateRsnapshot(in rsnapEvalInput, now time.Time) rsnapStatus {
	st := rsnapStatus{IntervalAges: map[string]float64{}}

	// Conf.Problems are faults rsnapshot itself would reject the file for:
	// the job never runs, so they must surface as a config error.
	st.ConfigError = !in.ConfReadable || in.Conf.SnapshotRoot == "" ||
		len(in.Conf.Intervals) == 0 || len(in.Conf.Problems) > 0
	if in.ConfReadable && in.Conf.SnapshotRoot != "" {
		st.RootMissing = !in.RootExists
		st.RootReadOnly = in.RootExists && in.RootReadOnly
	}

	// Lock: absent is idle; alive means the pid exists and runs rsnapshot.
	alive := in.Lock.Exists && !in.Lock.Empty && in.Lock.Pid > 0 && in.Lock.PidAlive && in.Lock.CmdlineMatch
	if in.Lock.Exists && !alive {
		if !in.Lock.Empty || now.Sub(in.Lock.Mtime) >= rsnapEmptyLockGrace {
			st.StaleLock = true
		}
	}
	st.Running = alive
	st.Stuck = alive && in.StuckAfter > 0 && now.Sub(in.Lock.Mtime) > in.StuckAfter

	// Last result; an incomplete run resolves via lock liveness. Fresh log
	// evidence with no lock stays indeterminate: the run may have written its
	// completion banner and unlinked the lock between the two reads.
	switch in.Log.LastResult {
	case "success", "warnings", "errors":
		st.LastResult = in.Log.LastResult
	case "incomplete":
		switch {
		case alive:
			st.LastResult = "running"
		case !in.LogMtime.IsZero() && now.Sub(in.LogMtime) < rsnapEmptyLockGrace:
			st.LastResult = "unknown"
		default:
			st.LastResult = "died"
		}
	default:
		st.LastResult = "unknown"
	}

	// Interval ages: mtime for the lowest level (rsnapshot touches it on
	// success), ctime for the rotate-only higher levels (mv preserves mtime).
	lowest := ""
	if len(in.Conf.Intervals) > 0 {
		lowest = in.Conf.Intervals[0].Name
	}
	var lowestMtime time.Time
	for i, iv := range in.Conf.Intervals {
		dt, ok := in.IntervalDirs[iv.Name]
		if !ok {
			continue
		}
		ts := dt.Ctime
		if i == 0 {
			ts = dt.Mtime
			lowestMtime = dt.Mtime
		}
		if ts.IsZero() {
			continue
		}
		st.IntervalAges[iv.Name] = math.Round(now.Sub(ts).Hours()*10) / 10
	}
	lowestSeen := !lowestMtime.IsZero()
	if lowestSeen {
		st.LastSuccess = lowestMtime
	}

	// Staleness bound for the lowest interval. Precedence: an explicit MaxAge
	// override, then the systemd timer schedule (the current mechanism), then the
	// crontab (fallback for hosts not yet migrated). Cron and timers agree during
	// a cutover, so preferring the timer is safe.
	var bound time.Duration
	boundKnown := false
	if lowest != "" {
		if d, ok := in.MaxAge[lowest]; ok && d > 0 {
			bound, boundKnown = d, true
		} else if b, ok := rsnapshotTimerBound(in.TimerMatches[lowest]); ok {
			bound, boundKnown = b+in.Margin, true
		} else if b, ok := rsnapshotCronBound(in.CronMatches[lowest]); ok {
			bound, boundKnown = b+in.Margin, true
		}
	}
	st.StaleKnown = lowestSeen && boundKnown
	st.Stale = st.StaleKnown && now.Sub(lowestMtime) > bound

	// Dead-man freshness: every activity trace older than the bound means the
	// cron+wrapper chain is dead even without a .0 to judge. Needs at least one
	// non-zero timestamp so a fresh setup stays pending.
	deadman := false
	if boundKnown && !st.Stale {
		freshest := in.LogMtime
		if lowestMtime.After(freshest) {
			freshest = lowestMtime
		}
		if in.Lock.Exists && in.Lock.Mtime.After(freshest) {
			freshest = in.Lock.Mtime
		}
		deadman = !freshest.IsZero() && now.Sub(freshest) > bound
	}

	// Schedule summary and partial-schedule check across BOTH sources (systemd
	// timers and cron): an interval is covered if either schedules it. Some
	// intervals scheduled while others are not is a problem; zero matches
	// everywhere is not (the schedule may live elsewhere).
	var cronList, timerList, missing []string
	matched := 0
	for _, iv := range in.Conf.Intervals {
		ces := in.CronMatches[iv.Name]
		tes := in.TimerMatches[iv.Name]
		if len(ces) > 0 || len(tes) > 0 {
			matched++
		} else {
			missing = append(missing, iv.Name)
		}
		st.CronJobs += len(ces)
		for _, e := range ces {
			cronList = append(cronList, iv.Name+" "+e.Spec)
		}
		st.TimerJobs += len(tes)
		for _, u := range tes {
			timerList = append(timerList, iv.Name+" "+strings.Join(u.Calendars, ","))
		}
	}
	scheduleReadable := in.CronReadable || in.TimerReadable
	partialSchedule := scheduleReadable && matched > 0 && len(missing) > 0

	var reasons []string
	if !in.ConfReadable {
		reasons = append(reasons, "conf unreadable")
	} else {
		if in.Conf.SnapshotRoot == "" {
			reasons = append(reasons, "no snapshot_root")
		}
		if len(in.Conf.Intervals) == 0 {
			reasons = append(reasons, "no retain intervals")
		}
		reasons = append(reasons, in.Conf.Problems...)
		reasons = append(reasons, in.Conf.Notes...)
	}
	if st.RootMissing {
		reasons = append(reasons, "snapshot root missing")
	}
	if st.RootReadOnly {
		reasons = append(reasons, "snapshot root read-only")
	}
	if st.StaleLock {
		reasons = append(reasons, "stale lockfile")
	}
	if st.LastResult == "errors" {
		reasons = append(reasons, "last run: errors")
	}
	if st.LastResult == "died" {
		reasons = append(reasons, "last run died")
	}
	if partialSchedule {
		reasons = append(reasons, "no schedule for "+strings.Join(missing, ","))
	}
	if st.Stuck {
		reasons = append(reasons, fmt.Sprintf("lock held %.1fh", now.Sub(in.Lock.Mtime).Hours()))
	}
	if st.Stale {
		reasons = append(reasons, fmt.Sprintf("%s.0 age %.1fh over %.1fh bound", lowest, now.Sub(lowestMtime).Hours(), bound.Hours()))
	}
	if deadman {
		reasons = append(reasons, fmt.Sprintf("no activity in %.1fh bound", bound.Hours()))
	}
	if in.Log.LastResult == "warnings" && !in.Log.BenignOnly {
		reasons = append(reasons, "last run: warnings")
	}
	if in.StrayCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d stray items", in.StrayCount))
	}
	st.Reasons = reasons

	// State precedence: first match wins.
	switch {
	case st.ConfigError || st.RootMissing || st.RootReadOnly || st.StaleLock ||
		st.LastResult == "errors" || st.LastResult == "died" || partialSchedule:
		st.State = "error"
	case st.Stuck:
		st.State = "stuck"
	case st.Stale || deadman:
		st.State = "stale"
	case alive:
		st.State = "running"
	case !lowestSeen && in.LogReadable && scheduleReadable:
		st.State = "pending"
	case (in.Log.LastResult == "warnings" && !in.Log.BenignOnly) || in.StrayCount > 0:
		st.State = "warning"
	case !lowestSeen:
		st.State = "unknown"
	default:
		st.State = "ok"
	}
	st.Problem = st.State == "error" || st.State == "stale" || st.State == "stuck"

	var itv []string
	for _, iv := range in.Conf.Intervals {
		itv = append(itv, iv.Name+":"+strconv.Itoa(iv.Keep))
	}
	st.IntervalsText = rsnapClip(strings.Join(itv, " "))
	st.CronList = rsnapClip(strings.Join(cronList, "; "))
	st.TimerList = rsnapClip(strings.Join(timerList, "; "))

	mount, rw, conf, lock := "ok", "ok", "ok", "idle"
	if st.RootMissing {
		mount = "missing"
	}
	if st.RootReadOnly {
		rw = "ro"
	}
	if st.ConfigError {
		conf = "err"
	}
	switch {
	case st.Stuck:
		lock = "stuck"
	case alive:
		lock = "run"
	case st.StaleLock:
		lock = "stale"
	}
	cron := strconv.Itoa(st.CronJobs)
	if !in.CronReadable {
		cron = "n/a"
	}
	timer := strconv.Itoa(st.TimerJobs)
	if !in.TimerReadable {
		timer = "n/a"
	}
	st.Details = rsnapClip(fmt.Sprintf("mount:%s rw:%s conf:%s cron:%s timer:%s lock:%s stray:%d",
		mount, rw, conf, cron, timer, lock, in.StrayCount))
	return st
}
