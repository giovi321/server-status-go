package collector

import (
	"strings"
	"testing"
	"time"
)

// A clean successful hoursago run with normal rsync/echo chatter in between.
const rsnapLogSuccess = `[10/Jul/2026:00:00:01] /usr/bin/rsnapshot -c /etc/rsnapshot.conf hoursago: started
[10/Jul/2026:00:00:01] echo 21507 > /run/rsnapshot_serialize.pid
[10/Jul/2026:00:00:02] /usr/bin/rsync -a --delete --numeric-ids /home/ /media/backup/rsnapshot/hoursago.0/localhost/home/
[10/Jul/2026:00:04:12] /usr/bin/rsnapshot -c /etc/rsnapshot.conf hoursago: completed successfully
`

// A success followed by a later run whose only warning is the benign rsync
// code-24 vanished-files case.
const rsnapLogBenignWarn = `[10/Jul/2026:00:00:01] /usr/bin/rsnapshot -c /etc/rsnapshot.conf hoursago: started
[10/Jul/2026:00:03:40] /usr/bin/rsnapshot -c /etc/rsnapshot.conf hoursago: completed successfully
[10/Jul/2026:06:00:01] /usr/bin/rsnapshot -c /etc/rsnapshot.conf hoursago: started
[10/Jul/2026:06:02:11] WARNING: some files vanished before they could be transferred (code 24)
[10/Jul/2026:06:04:55] /usr/bin/rsnapshot -c /etc/rsnapshot.conf hoursago: completed, but with some warnings
`

// A run with an ERROR: line and an errors completion banner.
const rsnapLogErrors = `[10/Jul/2026:05:20:01] /usr/bin/rsnapshot -c /etc/rsnapshot.conf daysago: started
[10/Jul/2026:05:21:14] ERROR: /usr/bin/rsync returned 23 while processing /home/
[10/Jul/2026:05:25:02] /usr/bin/rsnapshot -c /etc/rsnapshot.conf daysago: completed, but with some errors
`

// A started banner with no completion.
const rsnapLogIncomplete = `[10/Jul/2026:12:00:01] /usr/bin/rsnapshot -c /etc/rsnapshot.conf hoursago: started
[10/Jul/2026:12:00:02] /usr/bin/rsync -a --delete /home/ /media/backup/rsnapshot/hoursago.0/localhost/home/
`

// Interleaved runs: daysago completes after hoursago started. The last
// STARTED block (hoursago) wins; the daysago completion closes its own block.
const rsnapLogInterleaved = `[10/Jul/2026:05:20:01] /usr/bin/rsnapshot -c /etc/rsnapshot.conf daysago: started
[10/Jul/2026:06:00:01] /usr/bin/rsnapshot -c /etc/rsnapshot.conf hoursago: started
[10/Jul/2026:06:10:44] /usr/bin/rsnapshot -c /etc/rsnapshot.conf daysago: completed successfully
`

// A tail starting mid-line: the truncated started banner must be dropped, so
// the orphan completion has no block and the result is unknown.
const rsnapLogMidLine = `c /etc/rsnapshot.conf hoursago: started
[10/Jul/2026:18:03:09] /usr/bin/rsnapshot -c /etc/rsnapshot.conf hoursago: completed successfully
`

// A run with a non-benign WARNING (rsync exit 23, partial transfer).
const rsnapLogNonBenignWarn = `[10/Jul/2026:05:20:01] /usr/bin/rsnapshot -c /etc/rsnapshot.conf daysago: started
[10/Jul/2026:05:22:31] WARNING: /usr/bin/rsync returned 23 while processing /var/log/
[10/Jul/2026:05:25:02] /usr/bin/rsnapshot -c /etc/rsnapshot.conf daysago: completed, but with some warnings
`

// An unparseable timestamp: classification still works, times stay zero.
const rsnapLogBadStamp = `[garbage] /usr/bin/rsnapshot -c /etc/rsnapshot.conf hoursago: started
[garbage] /usr/bin/rsnapshot -c /etc/rsnapshot.conf hoursago: completed successfully
`

func logTime(day, h, m, s int) time.Time {
	return time.Date(2026, time.July, day, h, m, s, 0, time.Local)
}

func TestClassifyRsnapshotLog(t *testing.T) {
	cases := []struct {
		name string
		tail string
		want rsnapLogState
	}{
		{"success", rsnapLogSuccess, rsnapLogState{
			LastInterval: "hoursago", LastResult: "success",
			LastStart: logTime(10, 0, 0, 1), LastEnd: logTime(10, 0, 4, 12),
		}},
		{"benign warning", rsnapLogBenignWarn, rsnapLogState{
			LastInterval: "hoursago", LastResult: "warnings", BenignOnly: true,
			LastStart: logTime(10, 6, 0, 1), LastEnd: logTime(10, 6, 4, 55),
		}},
		{"errors", rsnapLogErrors, rsnapLogState{
			LastInterval: "daysago", LastResult: "errors",
			LastStart: logTime(10, 5, 20, 1), LastEnd: logTime(10, 5, 25, 2),
		}},
		{"incomplete", rsnapLogIncomplete, rsnapLogState{
			LastInterval: "hoursago", LastResult: "incomplete",
			LastStart: logTime(10, 12, 0, 1),
		}},
		{"interleaved last started wins", rsnapLogInterleaved, rsnapLogState{
			LastInterval: "hoursago", LastResult: "incomplete",
			LastStart: logTime(10, 6, 0, 1),
		}},
		{"empty", "", rsnapLogState{LastResult: "unknown"}},
		{"mid-line tail", rsnapLogMidLine, rsnapLogState{LastResult: "unknown"}},
		{"non-benign warning", rsnapLogNonBenignWarn, rsnapLogState{
			LastInterval: "daysago", LastResult: "warnings",
			LastStart: logTime(10, 5, 20, 1), LastEnd: logTime(10, 5, 25, 2),
		}},
		{"unparseable timestamp", rsnapLogBadStamp, rsnapLogState{
			LastInterval: "hoursago", LastResult: "success",
		}},
	}
	for _, c := range cases {
		got := classifyRsnapshotLog(c.tail, "/etc/rsnapshot.conf")
		if got.LastInterval != c.want.LastInterval || got.LastResult != c.want.LastResult || got.BenignOnly != c.want.BenignOnly {
			t.Fatalf("%s: got %+v want %+v", c.name, got, c.want)
		}
		if !got.LastStart.Equal(c.want.LastStart) || !got.LastEnd.Equal(c.want.LastEnd) {
			t.Fatalf("%s: times got start=%v end=%v want start=%v end=%v",
				c.name, got.LastStart, got.LastEnd, c.want.LastStart, c.want.LastEnd)
		}
	}
}

// An ERROR: line forces errors even when the banner claims success.
func TestClassifyRsnapshotLogErrorOverridesBanner(t *testing.T) {
	tail := `[10/Jul/2026:05:20:01] /usr/bin/rsnapshot -c /etc/rsnapshot.conf daysago: started
[10/Jul/2026:05:21:14] ERROR: /bin/cp failed
[10/Jul/2026:05:25:02] /usr/bin/rsnapshot -c /etc/rsnapshot.conf daysago: completed successfully
`
	got := classifyRsnapshotLog(tail, "/etc/rsnapshot.conf")
	if got.LastResult != "errors" {
		t.Fatalf("result=%q, want errors", got.LastResult)
	}
}

// The shared GC03SRVR log after the 2026-10-31 split: groups a, b and c write
// one file with ISO banner timestamps. c fails, then a succeeds.
const rsnapLogShared = `[2026-10-31T02:05:00] /usr/bin/rsnapshot -c /etc/rsnapshot-b.conf daysago: started
[2026-10-31T02:05:00] echo 1500001 > /var/run/rsnapshot-b.pid
[2026-10-31T02:05:40] /usr/bin/rsnapshot -c /etc/rsnapshot-b.conf daysago: completed successfully
[2026-10-31T02:10:00] /usr/bin/rsnapshot -c /etc/rsnapshot-c.conf daysago: started
[2026-10-31T02:10:00] echo 1500002 > /var/run/rsnapshot-c.pid
[2026-10-31T02:11:30] ERROR: /usr/bin/rsync returned 23 while processing /media/storage1/backup/GC01PC_home/
[2026-10-31T02:12:00] /usr/bin/rsnapshot -c /etc/rsnapshot-c.conf daysago: completed, but with some errors
[2026-10-31T04:00:17] /usr/bin/rsnapshot -c /etc/rsnapshot-a.conf hoursago: started
[2026-10-31T04:00:17] echo 1641657 > /var/run/rsnapshot-a.pid
[2026-10-31T04:19:37] /usr/bin/rsnapshot -c /etc/rsnapshot-a.conf hoursago: completed successfully
`

func TestClassifyRsnapshotLogPerConfig(t *testing.T) {
	c := classifyRsnapshotLog(rsnapLogShared, "/etc/rsnapshot-c.conf")
	if c.LastResult != "errors" || c.LastInterval != "daysago" {
		t.Fatalf("c: %+v", c)
	}
	a := classifyRsnapshotLog(rsnapLogShared, "/etc/rsnapshot-a.conf")
	if a.LastResult != "success" || a.LastInterval != "hoursago" {
		t.Fatalf("a: %+v", a)
	}
	if want := time.Date(2026, 10, 31, 4, 0, 17, 0, time.Local); !a.LastStart.Equal(want) {
		t.Fatalf("a start %v", a.LastStart)
	}
	if want := time.Date(2026, 10, 31, 2, 12, 0, 0, time.Local); !c.LastActivity.Equal(want) {
		t.Fatalf("c activity %v", c.LastActivity)
	}
	if b := classifyRsnapshotLog(rsnapLogShared, "/etc/rsnapshot-b.conf"); b.LastResult != "success" || b.LastInterval != "daysago" {
		t.Fatalf("b: %+v", b)
	}
	if d := classifyRsnapshotLog(rsnapLogShared, "/etc/rsnapshot-vm.conf"); d.LastResult != "unknown" {
		t.Fatalf("vm: %+v", d)
	}
}

// A banner without -c is rsnapshot's default config and nobody else's.
func TestClassifyRsnapshotLogNoConfFlag(t *testing.T) {
	tail := `[2026-10-31T01:00:00] /usr/bin/rsnapshot hoursago: started
[2026-10-31T01:05:00] /usr/bin/rsnapshot hoursago: completed successfully
`
	if got := classifyRsnapshotLog(tail, "/etc/rsnapshot.conf"); got.LastResult != "success" {
		t.Fatalf("default conf: %+v", got)
	}
	if got := classifyRsnapshotLog(tail, "/etc/rsnapshot-a.conf"); got.LastResult != "unknown" {
		t.Fatalf("other conf credited: %+v", got)
	}
}

// Old-format lines from before an rsnapshot upgrade, then ISO lines: both
// parse and the later block wins.
func TestClassifyRsnapshotLogMixedFormats(t *testing.T) {
	tail := `[30/Oct/2026:22:00:01] /usr/bin/rsnapshot -c /etc/rsnapshot-a.conf hoursago: started
[30/Oct/2026:22:01:00] /usr/bin/rsnapshot -c /etc/rsnapshot-a.conf hoursago: completed, but with some errors
[2026-10-31T00:00:17] /usr/bin/rsnapshot -c /etc/rsnapshot-a.conf hoursago: started
[2026-10-31T00:01:34] /usr/bin/rsnapshot -c /etc/rsnapshot-a.conf hoursago: completed successfully
`
	got := classifyRsnapshotLog(tail, "/etc/rsnapshot-a.conf")
	if got.LastResult != "success" {
		t.Fatalf("result %+v", got)
	}
	if want := time.Date(2026, 10, 31, 0, 1, 34, 0, time.Local); !got.LastEnd.Equal(want) {
		t.Fatalf("end %v", got.LastEnd)
	}
	old := classifyRsnapshotLog(tail[:strings.Index(tail, "[2026-")], "/etc/rsnapshot-a.conf")
	if want := time.Date(2026, 10, 30, 22, 0, 1, 0, time.Local); !old.LastStart.Equal(want) {
		t.Fatalf("old start %v", old.LastStart)
	}
}
