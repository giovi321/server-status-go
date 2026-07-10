package collector

import (
	"testing"
	"time"
)

const gc01RootCrontab = `# m h dom mon dow command
MAILTO=root
0 */6 * * * flock -w 21600 /run/rsnapshot_serialize.lock timeout --signal=TERM --kill-after=10m 8h /home/programmi/rsnapshot_run.sh /etc/rsnapshot.conf hoursago /var/log/rsnapshot.log
20 5 * * * flock -w 21600 /run/rsnapshot_serialize.lock timeout --signal=TERM --kill-after=10m 8h /home/programmi/rsnapshot_run.sh /etc/rsnapshot.conf daysago /var/log/rsnapshot.log
10 5 * * 1 flock -w 21600 /run/rsnapshot_serialize.lock timeout --signal=TERM --kill-after=10m 8h /home/programmi/rsnapshot_run.sh /etc/rsnapshot.conf weeksago /var/log/rsnapshot.log
0 5 1 * * flock -w 21600 /run/rsnapshot_serialize.lock timeout --signal=TERM --kill-after=10m 8h /home/programmi/rsnapshot_run.sh /etc/rsnapshot.conf monthsago /var/log/rsnapshot.log
`

func TestMatchRsnapshotCronGC01(t *testing.T) {
	entries := parseCronFile(gc01RootCrontab, false, "/var/spool/cron/crontabs/root")
	if len(entries) != 4 {
		t.Fatalf("entries=%d want 4", len(entries))
	}
	intervals := []string{"hoursago", "daysago", "weeksago", "monthsago"}
	m := matchRsnapshotCron(entries, "/etc/rsnapshot.conf", true, nil, intervals)
	want := map[string]string{
		"hoursago":  "0 */6 * * *",
		"daysago":   "20 5 * * *",
		"weeksago":  "10 5 * * 1",
		"monthsago": "0 5 1 * *",
	}
	for _, iv := range intervals {
		got := m[iv]
		if len(got) != 1 {
			t.Fatalf("%s: %d matches, want 1", iv, len(got))
		}
		if got[0].Spec != want[iv] {
			t.Fatalf("%s: spec %q want %q", iv, got[0].Spec, want[iv])
		}
		if got[0].Source != "/var/spool/cron/crontabs/root" {
			t.Fatalf("%s: source %q", iv, got[0].Source)
		}
	}
}

const cronDFixture = `# /etc/cron.d/rsnapshot: crontab fragment for rsnapshot
# 0 */4 * * *   root    /usr/bin/rsnapshot hourly
# 30 3  * * *   root    /usr/bin/rsnapshot daily
# 0  3  * * 1   root    /usr/bin/rsnapshot weekly
# 30 2  1 * *   root    /usr/bin/rsnapshot monthly
PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin
0 */6 * * *	root	/usr/bin/rsnapshot hoursago
`

func TestParseCronFileSystemFormat(t *testing.T) {
	entries := parseCronFile(cronDFixture, true, "/etc/cron.d/rsnapshot")
	if len(entries) != 1 {
		t.Fatalf("entries=%d want 1 (commented block and env line must be skipped)", len(entries))
	}
	e := entries[0]
	if e.Spec != "0 */6 * * *" {
		t.Fatalf("spec=%q", e.Spec)
	}
	if e.Command != "/usr/bin/rsnapshot hoursago" {
		t.Fatalf("command=%q (user column must be consumed)", e.Command)
	}
	m := matchRsnapshotCron(entries, "/etc/rsnapshot.conf", true, nil, []string{"hourly", "daily", "weekly", "monthly", "hoursago"})
	if len(m["hoursago"]) != 1 {
		t.Fatalf("hoursago matches=%d want 1", len(m["hoursago"]))
	}
	for _, iv := range []string{"hourly", "daily", "weekly", "monthly"} {
		if len(m[iv]) != 0 {
			t.Fatalf("%s matched from a commented line", iv)
		}
	}
}

func TestMatchRsnapshotCronOtherConfig(t *testing.T) {
	line := "30 4 * * * /home/programmi/rsnapshot_run.sh /etc/rsnapshot-gc01srvr.conf daily /var/log/rsnapshot-gc01srvr.log\n"
	entries := parseCronFile(line, false, "/var/spool/cron/crontabs/root")
	if len(entries) != 1 {
		t.Fatalf("entries=%d want 1", len(entries))
	}
	// Explicit path to another config must not attribute to the default conf.
	m := matchRsnapshotCron(entries, "/etc/rsnapshot.conf", true, nil, []string{"hoursago", "daysago", "weeksago", "monthsago", "daily"})
	if len(m) != 0 {
		t.Fatalf("matched default conf: %v", m)
	}
	// But it does attribute to its own config.
	m = matchRsnapshotCron(entries, "/etc/rsnapshot-gc01srvr.conf", false, nil, []string{"daily", "weekly", "monthly"})
	if len(m["daily"]) != 1 {
		t.Fatalf("daily matches=%d want 1", len(m["daily"]))
	}
}

func TestMatchRsnapshotCronOtherMonitoredNonEtc(t *testing.T) {
	entries := []cronEntry{{
		Spec:    "0 3 * * *",
		Command: "rsnapshot -c /root/backup.conf daily",
		Source:  "/var/spool/cron/crontabs/root",
	}}
	// A monitored config outside /etc/rsnapshot* must not leak onto the
	// default conf just because its path escapes the /etc/rsnapshot guard.
	m := matchRsnapshotCron(entries, "/etc/rsnapshot.conf", true, []string{"/root/backup.conf"}, []string{"daily", "weekly", "monthly"})
	if len(m) != 0 {
		t.Fatalf("leaked onto default conf: %v", m)
	}
	// It still attributes to its own config.
	m = matchRsnapshotCron(entries, "/root/backup.conf", false, []string{"/etc/rsnapshot.conf"}, []string{"daily", "weekly", "monthly"})
	if len(m["daily"]) != 1 {
		t.Fatalf("daily matches=%d want 1", len(m["daily"]))
	}
}

func TestCronMaxGap(t *testing.T) {
	cases := []struct {
		spec string
		want time.Duration
		ok   bool
	}{
		{"0 */6 * * *", 6 * time.Hour, true},
		{"20 5 * * *", 24 * time.Hour, true},
		{"10 5 * * 1", 168 * time.Hour, true},
		{"0 5 1 * *", 744 * time.Hour, true},
		{"0 8,10 * * *", 22 * time.Hour, true},
		{"0 0-20/4 * * *", 4 * time.Hour, true},
		// Star-with-step dom/dow fields keep their value sets (vixie ANDs
		// them unless both fields are restricted).
		{"0 3 */2 * *", 48 * time.Hour, true},
		{"0 5 * * */2", 48 * time.Hour, true},
		{"0 5 1 * */2", 3672 * time.Hour, true},
		{"@daily", 24 * time.Hour, true},
		{"@reboot", 0, false},
		{"not a cron spec", 0, false},
	}
	for _, c := range cases {
		got, ok := cronMaxGap(c.spec)
		if ok != c.ok || got != c.want {
			t.Errorf("%q: got (%v, %v) want (%v, %v)", c.spec, got, ok, c.want, c.ok)
		}
	}
}

func TestRsnapshotCronBound(t *testing.T) {
	rootEntry := cronEntry{Spec: "20 5 * * *", Command: "rsnapshot daysago", Source: "/var/spool/cron/crontabs/root"}
	cronDEntry := cronEntry{Spec: "0 */6 * * *", Command: "rsnapshot daysago", Source: "/etc/cron.d/rsnapshot"}

	// Root crontab entries are authoritative: cron.d must not tighten the bound.
	bound, ok := rsnapshotCronBound([]cronEntry{cronDEntry, rootEntry})
	if !ok || bound != 24*time.Hour {
		t.Fatalf("mixed sources: got (%v, %v) want (24h, true)", bound, ok)
	}
	// Without root entries, other sources apply.
	bound, ok = rsnapshotCronBound([]cronEntry{cronDEntry})
	if !ok || bound != 6*time.Hour {
		t.Fatalf("cron.d only: got (%v, %v) want (6h, true)", bound, ok)
	}
	if _, ok := rsnapshotCronBound(nil); ok {
		t.Fatal("no entries must yield no bound")
	}
}
