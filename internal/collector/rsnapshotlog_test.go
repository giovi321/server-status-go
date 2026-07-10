package collector

import (
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
	return time.Date(2026, time.July, day, h, m, s, 0, time.UTC)
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
		got := classifyRsnapshotLog(c.tail)
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
	got := classifyRsnapshotLog(tail)
	if got.LastResult != "errors" {
		t.Fatalf("result=%q, want errors", got.LastResult)
	}
}
