package collector

import (
	"testing"
	"time"
)

const gc01HoursagoTimer = `[Unit]
Description=rsnapshot hoursago (every 6h) — GC01SRVR

[Timer]
OnCalendar=*-*-* 00/6:00:00
Persistent=true
Unit=rsnapshot@hoursago.service

[Install]
WantedBy=timers.target
`

const gc01MainService = `[Unit]
Description=rsnapshot backup — %i interval (GC01SRVR)
OnFailure=rsnapshot-notify-fail@%n.service

[Service]
Type=oneshot
ExecStart=/usr/bin/flock -w 21600 /run/rsnapshot_serialize.lock /home/programmi/rsnapshot_run.sh /etc/rsnapshot.conf %i /var/log/rsnapshot.log
TimeoutStartSec=15h
`

func TestParseTimerUnit(t *testing.T) {
	cals, unit := parseTimerUnit(gc01HoursagoTimer)
	if len(cals) != 1 || cals[0] != "*-*-* 00/6:00:00" {
		t.Fatalf("calendars=%v", cals)
	}
	if unit != "rsnapshot@hoursago.service" {
		t.Fatalf("unit=%q", unit)
	}
	// keys outside [Timer] must not be picked up
	stray := "[Service]\nOnCalendar=*-*-* 09:00:00\n[Install]\nUnit=wrong.service\n"
	if cals, unit := parseTimerUnit(stray); len(cals) != 0 || unit != "" {
		t.Fatalf("picked up keys outside [Timer]: cals=%v unit=%q", cals, unit)
	}
}

func TestExecStartConf(t *testing.T) {
	if got := execStartConf(gc01MainService); got != "/etc/rsnapshot.conf" {
		t.Fatalf("conf=%q want /etc/rsnapshot.conf", got)
	}
	archive := "[Service]\nExecStart=/usr/bin/flock -w 14400 /run/l /home/programmi/rsnapshot_run.sh /etc/rsnapshot-gc01srvr.conf %i /var/log/x.log\n"
	if got := execStartConf(archive); got != "/etc/rsnapshot-gc01srvr.conf" {
		t.Fatalf("conf=%q", got)
	}
	if got := execStartConf("[Service]\nExecStart=/bin/true\n"); got != "" {
		t.Fatalf("conf=%q want empty", got)
	}
}

func TestInstanceName(t *testing.T) {
	cases := map[string]string{
		"rsnapshot@hoursago.service":       "hoursago",
		"rsnapshot-gc01srvr@daily.service": "daily",
		"rsnapshot-stale.service":          "",
		"rsnapshot@monthsago":              "monthsago",
	}
	for in, want := range cases {
		if got := instanceName(in); got != want {
			t.Errorf("instanceName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestOnCalendarMaxGap(t *testing.T) {
	cases := []struct {
		spec string
		want time.Duration
		ok   bool
	}{
		// the exact OnCalendar values the migration deploys
		{"*-*-* 00/6:00:00", 6 * time.Hour, true},     // GC01 hoursago
		{"*-*-* 05:20:00", 24 * time.Hour, true},      // daysago
		{"Mon *-*-* 05:10:00", 168 * time.Hour, true}, // weeksago
		{"*-*-01 05:00:00", 744 * time.Hour, true},    // monthsago
		{"*-*-* 00/4:00:00", 4 * time.Hour, true},     // GC03 main hoursago
		{"*-*-* 02:50:00", 24 * time.Hour, true},      // archive daily
		{"*-*-* 00/2:30:00", 2 * time.Hour, true},     // stale check
		// shorthands
		{"daily", 24 * time.Hour, true},
		{"weekly", 168 * time.Hour, true},
		{"monthly", 744 * time.Hour, true},
		{"hourly", time.Hour, true},
		// unmappable / invalid
		{"2026-07-15 05:00:00", 0, false}, // specific year (one-shot)
		{"*-*-* *:*:00", time.Minute, true},
		{"not a calendar", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := onCalendarMaxGap(c.spec)
		if ok != c.ok || got != c.want {
			t.Errorf("%q: got (%v, %v) want (%v, %v)", c.spec, got, ok, c.want, c.ok)
		}
	}
}

func TestMatchRsnapshotTimersGC01(t *testing.T) {
	intervals := []string{"hoursago", "daysago", "weeksago", "monthsago"}
	units := []timerUnit{
		{Name: "rsnapshot-hoursago.timer", Calendars: []string{"*-*-* 00/6:00:00"}, Activates: "rsnapshot@hoursago.service", ConfPath: "/etc/rsnapshot.conf", Interval: "hoursago", Enabled: true},
		{Name: "rsnapshot-daysago.timer", Calendars: []string{"*-*-* 05:20:00"}, Activates: "rsnapshot@daysago.service", ConfPath: "/etc/rsnapshot.conf", Interval: "daysago", Enabled: true},
		{Name: "rsnapshot-weeksago.timer", Calendars: []string{"Mon *-*-* 05:10:00"}, Activates: "rsnapshot@weeksago.service", ConfPath: "/etc/rsnapshot.conf", Interval: "weeksago", Enabled: true},
		{Name: "rsnapshot-monthsago.timer", Calendars: []string{"*-*-01 05:00:00"}, Activates: "rsnapshot@monthsago.service", ConfPath: "/etc/rsnapshot.conf", Interval: "monthsago", Enabled: true},
		// noise: disabled, wrong config, non-rsnapshot, out-of-set interval
		{Name: "rsnapshot-stale.timer", Activates: "rsnapshot-stale.service", ConfPath: "", Interval: "", Enabled: true},
		{Name: "rsnapshot-disabled.timer", Activates: "rsnapshot@hoursago.service", ConfPath: "/etc/rsnapshot.conf", Interval: "hoursago", Enabled: false},
		{Name: "other.timer", Activates: "rsnapshot-gc01srvr@daily.service", ConfPath: "/etc/rsnapshot-gc01srvr.conf", Interval: "daily", Enabled: true},
	}
	m := matchRsnapshotTimers(units, "/etc/rsnapshot.conf", true, intervals)
	for _, iv := range intervals {
		if len(m[iv]) != 1 {
			t.Fatalf("%s: %d matches want 1", iv, len(m[iv]))
		}
	}
	if len(m) != len(intervals) {
		t.Fatalf("extra matches leaked in: %v", m)
	}
}

func TestMatchRsnapshotTimersArchiveConfig(t *testing.T) {
	units := []timerUnit{
		{Name: "rsnapshot-gc01srvr-daily.timer", Activates: "rsnapshot-gc01srvr@daily.service", ConfPath: "/etc/rsnapshot-gc01srvr.conf", Interval: "daily", Enabled: true},
		{Name: "rsnapshot-hoursago.timer", Activates: "rsnapshot@hoursago.service", ConfPath: "/etc/rsnapshot.conf", Interval: "hoursago", Enabled: true},
	}
	// The archive timer must not attribute to the default conf...
	if m := matchRsnapshotTimers(units, "/etc/rsnapshot.conf", true, []string{"hoursago", "daysago"}); len(m["daily"]) != 0 {
		t.Fatalf("archive timer leaked onto default conf: %v", m)
	}
	// ...but does attribute to its own conf.
	m := matchRsnapshotTimers(units, "/etc/rsnapshot-gc01srvr.conf", false, []string{"daily", "weekly", "monthly"})
	if len(m["daily"]) != 1 {
		t.Fatalf("daily matches=%d want 1", len(m["daily"]))
	}
	if len(m["hoursago"]) != 0 {
		t.Fatalf("default-conf timer leaked onto archive conf")
	}
}

func TestMatchRsnapshotTimersUnresolvedConf(t *testing.T) {
	// A timer whose service conf could not be read only defaults to the main config.
	units := []timerUnit{{Name: "rsnapshot-hoursago.timer", Activates: "rsnapshot@hoursago.service", ConfPath: "", Interval: "hoursago", Enabled: true}}
	if m := matchRsnapshotTimers(units, "/etc/rsnapshot.conf", true, []string{"hoursago"}); len(m["hoursago"]) != 1 {
		t.Fatalf("unresolved conf should default to main: %v", m)
	}
	if m := matchRsnapshotTimers(units, "/etc/rsnapshot-gc01srvr.conf", false, []string{"hoursago"}); len(m) != 0 {
		t.Fatalf("unresolved conf must not attribute to a non-default conf: %v", m)
	}
}

func TestRsnapshotTimerBound(t *testing.T) {
	// tightest timer wins
	units := []timerUnit{
		{Calendars: []string{"*-*-* 05:20:00"}},   // 24h
		{Calendars: []string{"*-*-* 00/6:00:00"}}, // 6h
	}
	if b, ok := rsnapshotTimerBound(units); !ok || b != 6*time.Hour {
		t.Fatalf("got (%v, %v) want (6h, true)", b, ok)
	}
	// within a timer, the smallest per-calendar gap (union) wins
	multi := []timerUnit{{Calendars: []string{"*-*-01 05:00:00", "*-*-* 00/6:00:00"}}}
	if b, ok := rsnapshotTimerBound(multi); !ok || b != 6*time.Hour {
		t.Fatalf("union: got (%v, %v) want (6h, true)", b, ok)
	}
	// unparseable calendars yield no bound
	if _, ok := rsnapshotTimerBound([]timerUnit{{Calendars: []string{"garbage"}}}); ok {
		t.Fatal("unparseable calendar must yield no bound")
	}
	if _, ok := rsnapshotTimerBound(nil); ok {
		t.Fatal("no units must yield no bound")
	}
}
