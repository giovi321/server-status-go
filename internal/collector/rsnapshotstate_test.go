package collector

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var rsnapNow = time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

const rsnapRootTab = "/var/spool/cron/crontabs/root"

// gc01EvalInput mirrors the GC01SRVR shape: four custom retain levels driven
// by four root-crontab entries through the flock+timeout wrapper. All healthy.
func gc01EvalInput() rsnapEvalInput {
	wrap := func(interval string) string {
		return "flock -w 21600 /run/rsnapshot_serialize.lock timeout --signal=TERM --kill-after=10m 8h /home/programmi/rsnapshot_run.sh /etc/rsnapshot.conf " + interval + " /var/log/rsnapshot.log"
	}
	return rsnapEvalInput{
		Conf: rsnapConf{
			Path:         "/etc/rsnapshot.conf",
			SnapshotRoot: "/media/backup/rsnapshot/",
			Lockfile:     "/var/run/rsnapshot.pid",
			Logfile:      "/var/log/rsnapshot.log",
			Intervals: []rsnapInterval{
				{Name: "hoursago", Keep: 6},
				{Name: "daysago", Keep: 7},
				{Name: "weeksago", Keep: 4},
				{Name: "monthsago", Keep: 4},
			},
			BackupLines: 3,
		},
		ConfReadable: true,
		Log: rsnapLogState{
			LastInterval: "hoursago",
			LastResult:   "success",
			LastStart:    rsnapNow.Add(-3*time.Hour - 10*time.Minute),
			LastEnd:      rsnapNow.Add(-3 * time.Hour),
		},
		LogReadable: true,
		LogMtime:    rsnapNow.Add(-3 * time.Hour),
		IntervalDirs: map[string]rsnapDirTimes{
			"hoursago":  {Mtime: rsnapNow.Add(-3 * time.Hour), Ctime: rsnapNow.Add(-3 * time.Hour)},
			"daysago":   {Mtime: rsnapNow.Add(-31 * time.Hour), Ctime: rsnapNow.Add(-7 * time.Hour)},
			"weeksago":  {Mtime: rsnapNow.Add(-200 * time.Hour), Ctime: rsnapNow.Add(-79 * time.Hour)},
			"monthsago": {Mtime: rsnapNow.Add(-900 * time.Hour), Ctime: rsnapNow.Add(-223 * time.Hour)},
		},
		CronMatches: map[string][]cronEntry{
			"hoursago":  {{Spec: "0 */6 * * *", Command: wrap("hoursago"), Source: rsnapRootTab}},
			"daysago":   {{Spec: "20 5 * * *", Command: wrap("daysago"), Source: rsnapRootTab}},
			"weeksago":  {{Spec: "10 5 * * 1", Command: wrap("weeksago"), Source: rsnapRootTab}},
			"monthsago": {{Spec: "0 5 1 * *", Command: wrap("monthsago"), Source: rsnapRootTab}},
		},
		CronReadable: true,
		RootExists:   true,
		Margin:       8 * time.Hour,
		StuckAfter:   12 * time.Hour,
	}
}

// gc01Timers mirrors the migrated GC01SRVR shape: the same four intervals driven
// by systemd timers instead of cron.
func gc01Timers() map[string][]timerUnit {
	mk := func(interval, cal string) []timerUnit {
		return []timerUnit{{
			Name:      "rsnapshot-" + interval + ".timer",
			Calendars: []string{cal},
			Activates: "rsnapshot@" + interval + ".service",
			ConfPath:  "/etc/rsnapshot.conf",
			Interval:  interval,
			Enabled:   true,
		}}
	}
	return map[string][]timerUnit{
		"hoursago":  mk("hoursago", "*-*-* 00/6:00:00"),
		"daysago":   mk("daysago", "*-*-* 05:20:00"),
		"weeksago":  mk("weeksago", "Mon *-*-* 05:10:00"),
		"monthsago": mk("monthsago", "*-*-01 05:00:00"),
	}
}

func TestEvaluateRsnapshot(t *testing.T) {
	aliveLock := func(age time.Duration) rsnapLockState {
		return rsnapLockState{Exists: true, Pid: 4242, PidAlive: true, CmdlineMatch: true, Mtime: rsnapNow.Add(-age)}
	}
	cases := []struct {
		name  string
		mut   func(in *rsnapEvalInput)
		state string
		check func(t *testing.T, st rsnapStatus)
	}{
		{
			name:  "all good gc01",
			state: "ok",
			check: func(t *testing.T, st rsnapStatus) {
				if st.LastResult != "success" || st.Stale || st.Stuck || st.Running {
					t.Fatalf("flags: %+v", st)
				}
				if !st.StaleKnown {
					t.Fatal("StaleKnown must be true with a cron-derived bound")
				}
				if !st.LastSuccess.Equal(rsnapNow.Add(-3 * time.Hour)) {
					t.Fatalf("LastSuccess: %v", st.LastSuccess)
				}
				want := map[string]float64{"hoursago": 3.0, "daysago": 7.0, "weeksago": 79.0, "monthsago": 223.0}
				if len(st.IntervalAges) != len(want) {
					t.Fatalf("ages: %v", st.IntervalAges)
				}
				for k, v := range want {
					if st.IntervalAges[k] != v {
						t.Fatalf("age %s = %v, want %v", k, st.IntervalAges[k], v)
					}
				}
				if st.CronJobs != 4 {
					t.Fatalf("CronJobs: %d", st.CronJobs)
				}
				if st.IntervalsText != "hoursago:6 daysago:7 weeksago:4 monthsago:4" {
					t.Fatalf("IntervalsText: %q", st.IntervalsText)
				}
				if st.CronList != "hoursago 0 */6 * * *; daysago 20 5 * * *; weeksago 10 5 * * 1; monthsago 0 5 1 * *" {
					t.Fatalf("CronList: %q", st.CronList)
				}
				if st.Details != "mount:ok rw:ok conf:ok cron:4 timer:n/a lock:idle stray:0" {
					t.Fatalf("Details: %q", st.Details)
				}
				if len(st.Reasons) != 0 {
					t.Fatalf("Reasons: %v", st.Reasons)
				}
			},
		},
		{
			name: "all good via timers (migrated off cron)",
			mut: func(in *rsnapEvalInput) {
				in.CronMatches = nil
				in.TimerReadable = true
				in.TimerMatches = gc01Timers()
			},
			state: "ok",
			check: func(t *testing.T, st rsnapStatus) {
				if !st.StaleKnown {
					t.Fatal("StaleKnown must be true with a timer-derived bound")
				}
				if st.Stale || st.Problem {
					t.Fatalf("fresh timer-scheduled backup must be ok: %+v", st)
				}
				if st.TimerJobs != 4 || st.CronJobs != 0 {
					t.Fatalf("jobs: timer=%d cron=%d", st.TimerJobs, st.CronJobs)
				}
				if !strings.Contains(st.Details, "timer:4") {
					t.Fatalf("Details: %q", st.Details)
				}
			},
		},
		{
			name: "stale lowest via timer bound",
			mut: func(in *rsnapEvalInput) {
				in.CronMatches = nil
				in.TimerReadable = true
				in.TimerMatches = gc01Timers()
				// bound = 6h gap + 8h margin = 14h; make hoursago.0 older than that
				old := rsnapNow.Add(-20 * time.Hour)
				in.IntervalDirs["hoursago"] = rsnapDirTimes{Mtime: old, Ctime: old}
				in.LogMtime = old
			},
			state: "stale",
			check: func(t *testing.T, st rsnapStatus) {
				if !st.Stale {
					t.Fatalf("expected stale via timer bound: %+v", st)
				}
			},
		},
		{
			name: "partial timer schedule is an error",
			mut: func(in *rsnapEvalInput) {
				in.CronMatches = nil
				in.TimerReadable = true
				in.TimerMatches = map[string][]timerUnit{"hoursago": gc01Timers()["hoursago"]}
			},
			state: "error",
			check: func(t *testing.T, st rsnapStatus) {
				found := false
				for _, r := range st.Reasons {
					if strings.Contains(r, "no schedule for") {
						found = true
					}
				}
				if !found {
					t.Fatalf("expected 'no schedule for' reason: %v", st.Reasons)
				}
			},
		},
		{
			name: "stale lowest via max_age",
			mut: func(in *rsnapEvalInput) {
				in.MaxAge = map[string]time.Duration{"hoursago": 6 * time.Hour}
				in.IntervalDirs["hoursago"] = rsnapDirTimes{Mtime: rsnapNow.Add(-20 * time.Hour)}
				in.LogMtime = rsnapNow.Add(-20 * time.Hour)
			},
			state: "stale",
			check: func(t *testing.T, st rsnapStatus) {
				if !st.StaleKnown || !st.Stale {
					t.Fatalf("StaleKnown=%v Stale=%v", st.StaleKnown, st.Stale)
				}
			},
		},
		{
			name: "stale lowest via cron bound",
			mut: func(in *rsnapEvalInput) {
				// Bound = 6h max gap + 8h margin = 14h; 20h-old .0 is stale.
				in.IntervalDirs["hoursago"] = rsnapDirTimes{Mtime: rsnapNow.Add(-20 * time.Hour)}
				in.LogMtime = rsnapNow.Add(-20 * time.Hour)
			},
			state: "stale",
			check: func(t *testing.T, st rsnapStatus) {
				if !st.Stale {
					t.Fatal("Stale must be set")
				}
			},
		},
		{
			name: "age within cron bound",
			mut: func(in *rsnapEvalInput) {
				in.IntervalDirs["hoursago"] = rsnapDirTimes{Mtime: rsnapNow.Add(-5 * time.Hour)}
			},
			state: "ok",
		},
		{
			name: "max_age override tighter than cron flips stale",
			mut: func(in *rsnapEvalInput) {
				in.IntervalDirs["hoursago"] = rsnapDirTimes{Mtime: rsnapNow.Add(-5 * time.Hour)}
				in.MaxAge = map[string]time.Duration{"hoursago": 2 * time.Hour}
			},
			state: "stale",
		},
		{
			name: "no cron no override",
			mut: func(in *rsnapEvalInput) {
				in.CronMatches = nil
			},
			state: "ok",
			check: func(t *testing.T, st rsnapStatus) {
				if st.StaleKnown || st.Stale {
					t.Fatalf("StaleKnown=%v Stale=%v without a bound", st.StaleKnown, st.Stale)
				}
				if st.CronJobs != 0 {
					t.Fatalf("CronJobs: %d", st.CronJobs)
				}
			},
		},
		{
			name:  "stuck lock alive and old",
			mut:   func(in *rsnapEvalInput) { in.Lock = aliveLock(20 * time.Hour) },
			state: "stuck",
			check: func(t *testing.T, st rsnapStatus) {
				if !st.Stuck || !st.Running || st.StaleLock {
					t.Fatalf("flags: %+v", st)
				}
			},
		},
		{
			name: "stale lock dead pid",
			mut: func(in *rsnapEvalInput) {
				in.Lock = rsnapLockState{Exists: true, Pid: 4242, Mtime: rsnapNow.Add(-2 * time.Hour)}
			},
			state: "error",
			check: func(t *testing.T, st rsnapStatus) {
				if !st.StaleLock || st.Running {
					t.Fatalf("flags: %+v", st)
				}
			},
		},
		{
			name: "stale lock pid reuse",
			mut: func(in *rsnapEvalInput) {
				in.Lock = rsnapLockState{Exists: true, Pid: 4242, PidAlive: true, CmdlineMatch: false, Mtime: rsnapNow.Add(-2 * time.Hour)}
			},
			state: "error",
			check: func(t *testing.T, st rsnapStatus) {
				if !st.StaleLock {
					t.Fatal("pid reuse must count as stale lock")
				}
			},
		},
		{
			name: "empty lock young is indeterminate",
			mut: func(in *rsnapEvalInput) {
				in.Lock = rsnapLockState{Exists: true, Empty: true, Mtime: rsnapNow.Add(-2 * time.Minute)}
			},
			state: "ok",
			check: func(t *testing.T, st rsnapStatus) {
				if st.StaleLock {
					t.Fatal("young empty lock must be ignored")
				}
			},
		},
		{
			name: "empty lock old is stale lock",
			mut: func(in *rsnapEvalInput) {
				in.Lock = rsnapLockState{Exists: true, Empty: true, Mtime: rsnapNow.Add(-time.Hour)}
			},
			state: "error",
			check: func(t *testing.T, st rsnapStatus) {
				if !st.StaleLock {
					t.Fatal("old empty lock must be stale")
				}
			},
		},
		{
			name: "running incomplete with live lock",
			mut: func(in *rsnapEvalInput) {
				in.Log.LastResult = "incomplete"
				in.Lock = aliveLock(30 * time.Minute)
			},
			state: "running",
			check: func(t *testing.T, st rsnapStatus) {
				if st.LastResult != "running" || !st.Running {
					t.Fatalf("LastResult=%q Running=%v", st.LastResult, st.Running)
				}
			},
		},
		{
			name: "died incomplete with absent lock",
			mut: func(in *rsnapEvalInput) {
				in.Log.LastResult = "incomplete"
				in.Lock = rsnapLockState{}
			},
			state: "error",
			check: func(t *testing.T, st rsnapStatus) {
				if st.LastResult != "died" {
					t.Fatalf("LastResult: %q", st.LastResult)
				}
			},
		},
		{
			name: "incomplete with fresh log and no lock stays indeterminate",
			mut: func(in *rsnapEvalInput) {
				// The run may have banner-completed and unlinked the lock
				// between the log and lock reads: no death verdict yet.
				in.Log.LastResult = "incomplete"
				in.Lock = rsnapLockState{}
				in.LogMtime = rsnapNow.Add(-30 * time.Second)
			},
			state: "ok",
			check: func(t *testing.T, st rsnapStatus) {
				if st.LastResult != "unknown" {
					t.Fatalf("LastResult: %q", st.LastResult)
				}
			},
		},
		{
			name: "died incomplete with dead lock",
			mut: func(in *rsnapEvalInput) {
				in.Log.LastResult = "incomplete"
				in.Lock = rsnapLockState{Exists: true, Pid: 4242, Mtime: rsnapNow.Add(-time.Hour)}
			},
			state: "error",
			check: func(t *testing.T, st rsnapStatus) {
				if st.LastResult != "died" || !st.StaleLock {
					t.Fatalf("LastResult=%q StaleLock=%v", st.LastResult, st.StaleLock)
				}
			},
		},
		{
			name:  "last run errors",
			mut:   func(in *rsnapEvalInput) { in.Log.LastResult = "errors" },
			state: "error",
			check: func(t *testing.T, st rsnapStatus) {
				if st.LastResult != "errors" {
					t.Fatalf("LastResult: %q", st.LastResult)
				}
			},
		},
		{
			name: "pending ramp-up",
			mut: func(in *rsnapEvalInput) {
				in.IntervalDirs = nil
				in.Log = rsnapLogState{LastResult: "unknown"}
				in.LogMtime = time.Time{}
			},
			state: "pending",
			check: func(t *testing.T, st rsnapStatus) {
				if st.StaleKnown || st.Stale || !st.LastSuccess.IsZero() {
					t.Fatalf("flags: %+v", st)
				}
				if len(st.IntervalAges) != 0 {
					t.Fatalf("ages: %v", st.IntervalAges)
				}
			},
		},
		{
			name: "dead-man no dirs but old log",
			mut: func(in *rsnapEvalInput) {
				in.IntervalDirs = nil
				in.Log = rsnapLogState{LastResult: "unknown"}
				in.LogMtime = rsnapNow.Add(-240 * time.Hour)
				in.MaxAge = map[string]time.Duration{"hoursago": 24 * time.Hour}
			},
			state: "stale",
			check: func(t *testing.T, st rsnapStatus) {
				if st.StaleKnown || st.Stale {
					t.Fatal("dead-man staleness must not assert the stale binary")
				}
			},
		},
		{
			name: "partial cron",
			mut: func(in *rsnapEvalInput) {
				delete(in.CronMatches, "daysago")
			},
			state: "error",
			check: func(t *testing.T, st rsnapStatus) {
				if st.CronJobs != 3 {
					t.Fatalf("CronJobs: %d", st.CronJobs)
				}
				if !strings.Contains(strings.Join(st.Reasons, " "), "daysago") {
					t.Fatalf("reason must name daysago: %v", st.Reasons)
				}
			},
		},
		{
			name: "benign warnings stay ok",
			mut: func(in *rsnapEvalInput) {
				in.Log.LastResult = "warnings"
				in.Log.BenignOnly = true
			},
			state: "ok",
			check: func(t *testing.T, st rsnapStatus) {
				if st.LastResult != "warnings" {
					t.Fatalf("LastResult: %q", st.LastResult)
				}
			},
		},
		{
			name: "non-benign warnings",
			mut: func(in *rsnapEvalInput) {
				in.Log.LastResult = "warnings"
			},
			state: "warning",
		},
		{
			name:  "strays warn",
			mut:   func(in *rsnapEvalInput) { in.StrayCount = 2 },
			state: "warning",
			check: func(t *testing.T, st rsnapStatus) {
				if !strings.Contains(st.Details, "stray:2") {
					t.Fatalf("Details: %q", st.Details)
				}
			},
		},
		{
			name: "root missing",
			mut: func(in *rsnapEvalInput) {
				in.RootExists = false
				in.IntervalDirs = nil
			},
			state: "error",
			check: func(t *testing.T, st rsnapStatus) {
				if !st.RootMissing {
					t.Fatal("RootMissing must be set")
				}
				if !strings.Contains(st.Details, "mount:missing") {
					t.Fatalf("Details: %q", st.Details)
				}
			},
		},
		{
			name:  "root read-only",
			mut:   func(in *rsnapEvalInput) { in.RootReadOnly = true },
			state: "error",
			check: func(t *testing.T, st rsnapStatus) {
				if !st.RootReadOnly {
					t.Fatal("RootReadOnly must be set")
				}
			},
		},
		{
			name: "conf unreadable",
			mut: func(in *rsnapEvalInput) {
				in.ConfReadable = false
				in.Conf = rsnapConf{Path: "/etc/rsnapshot.conf"}
			},
			state: "error",
			check: func(t *testing.T, st rsnapStatus) {
				if !st.ConfigError {
					t.Fatal("ConfigError must be set")
				}
			},
		},
		{
			name: "fatal conf problem is config error",
			mut: func(in *rsnapEvalInput) {
				// A space-separated conf is fatal to rsnapshot itself: the
				// job never runs, so nothing else would ever flag it.
				in.Conf.Problems = []string{"space-separated directives (rsnapshot requires tabs)"}
				in.IntervalDirs = nil
				in.Log = rsnapLogState{}
				in.LogMtime = time.Time{}
			},
			state: "error",
			check: func(t *testing.T, st rsnapStatus) {
				if !st.ConfigError {
					t.Fatal("ConfigError must be set")
				}
				if !strings.Contains(strings.Join(st.Reasons, " "), "tabs") {
					t.Fatalf("reason must carry the problem text: %v", st.Reasons)
				}
				if !strings.Contains(st.Details, "conf:err") {
					t.Fatalf("Details: %q", st.Details)
				}
			},
		},
		{
			name: "conf notes stay non-fatal",
			mut: func(in *rsnapEvalInput) {
				in.Conf.Notes = []string{"include_conf not followed"}
			},
			state: "ok",
			check: func(t *testing.T, st rsnapStatus) {
				if st.ConfigError || st.Problem {
					t.Fatalf("flags: %+v", st)
				}
				if !strings.Contains(strings.Join(st.Reasons, " "), "include_conf") {
					t.Fatalf("note must surface in reasons: %v", st.Reasons)
				}
			},
		},
		{
			name: "no intervals is config error",
			mut: func(in *rsnapEvalInput) {
				in.Conf.Intervals = nil
				in.IntervalDirs = nil
				in.CronMatches = nil
			},
			state: "error",
			check: func(t *testing.T, st rsnapStatus) {
				if !st.ConfigError {
					t.Fatal("ConfigError must be set")
				}
			},
		},
		{
			name: "unknown when log unreadable and no dot-zero",
			mut: func(in *rsnapEvalInput) {
				in.IntervalDirs = nil
				in.Log = rsnapLogState{}
				in.LogReadable = false
				in.LogMtime = time.Time{}
				in.CronMatches = nil
			},
			state: "unknown",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := gc01EvalInput()
			if tc.mut != nil {
				tc.mut(&in)
			}
			st := evaluateRsnapshot(in, rsnapNow)
			if st.State != tc.state {
				t.Fatalf("state = %q, want %q (reasons %v)", st.State, tc.state, st.Reasons)
			}
			wantProblem := st.State == "error" || st.State == "stale" || st.State == "stuck"
			if st.Problem != wantProblem {
				t.Fatalf("Problem = %v for state %q", st.Problem, st.State)
			}
			if tc.check != nil {
				tc.check(t, st)
			}
		})
	}
}

// The zero-value input must evaluate without panicking (nil maps included).
func TestEvaluateRsnapshotZeroInput(t *testing.T) {
	st := evaluateRsnapshot(rsnapEvalInput{}, rsnapNow)
	if st.State != "error" || !st.Problem || !st.ConfigError {
		t.Fatalf("zero input: %+v", st)
	}
}

// Text fields stay within the 255-char HA limit even with many long intervals.
func TestEvaluateRsnapshotTextCaps(t *testing.T) {
	in := gc01EvalInput()
	in.Conf.Intervals = nil
	in.IntervalDirs = map[string]rsnapDirTimes{}
	in.CronMatches = map[string][]cronEntry{}
	long := strings.Repeat("verylongintervalname", 3)
	for i := 0; i < 10; i++ {
		n := fmt.Sprintf("%s%d", long, i)
		in.Conf.Intervals = append(in.Conf.Intervals, rsnapInterval{Name: n, Keep: 12345})
		in.IntervalDirs[n] = rsnapDirTimes{Mtime: rsnapNow.Add(-time.Hour), Ctime: rsnapNow.Add(-time.Hour)}
		in.CronMatches[n] = []cronEntry{{Spec: "0 */6 * * *", Source: rsnapRootTab}}
	}
	st := evaluateRsnapshot(in, rsnapNow)
	for name, v := range map[string]string{
		"State":         st.State,
		"LastResult":    st.LastResult,
		"CronList":      st.CronList,
		"IntervalsText": st.IntervalsText,
		"Details":       st.Details,
	} {
		if len(v) > 255 {
			t.Fatalf("%s is %d chars", name, len(v))
		}
	}
	if !strings.HasSuffix(st.CronList, "...") || !strings.HasSuffix(st.IntervalsText, "...") {
		t.Fatalf("truncation marker missing: %q / %q", st.CronList, st.IntervalsText)
	}
}

// The truncation cut must never split a multi-byte rune.
func TestRsnapClipRuneBoundary(t *testing.T) {
	// 1 ASCII byte + 130 two-byte runes = 261 bytes; byte 252 lands mid-rune.
	s := "x" + strings.Repeat("ä", 130)
	got := rsnapClip(s)
	if len(got) > rsnapTextLimit {
		t.Fatalf("len=%d", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("marker missing: %q", got)
	}
	// Pure ASCII keeps the full width.
	if got := rsnapClip(strings.Repeat("a", 300)); len(got) != rsnapTextLimit {
		t.Fatalf("ascii len=%d", len(got))
	}
	if short := rsnapClip("short"); short != "short" {
		t.Fatalf("short: %q", short)
	}
}
