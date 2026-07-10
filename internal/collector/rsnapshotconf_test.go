package collector

import (
	"reflect"
	"strings"
	"testing"
)

// GC01SRVR /etc/rsnapshot.conf shape: custom retain names, tabs, comments, blanks.
const gc01Conf = "# /etc/rsnapshot.conf\n" +
	"config_version\t1.2\n" +
	"\n" +
	"snapshot_root\t/media/backup/rsnapshot/\n" +
	"\n" +
	"cmd_rsync\t/usr/bin/rsync\n" +
	"lockfile\t/var/run/rsnapshot.pid\n" +
	"\n" +
	"# Retain levels: file order = frequency order\n" +
	"retain\thoursago\t6\n" +
	"retain\tdaysago\t7\n" +
	"retain\tweeksago\t4\n" +
	"retain\tmonthsago\t4\n" +
	"\n" +
	"verbose\t2\n" +
	"loglevel\t3\n" +
	"logfile\t/var/log/rsnapshot.log\n" +
	"\n" +
	"# What to back up\n" +
	"backup\t/home/\thome/\n" +
	"backup\t/etc/\tetc/\n" +
	"backup\t/var/lib/\tvarlib/\n"

// GC03SRVR /etc/rsnapshot-gc01srvr.conf shape: own root/log/lock, sync_first, lazy deletes.
const gc03Gc01srvrConf = "config_version\t1.2\n" +
	"snapshot_root\t/media/backup/rsnapshot-gc01srvr/\n" +
	"lockfile\t/var/run/rsnapshot-gc01srvr.pid\n" +
	"logfile\t/var/log/rsnapshot-gc01srvr.log\n" +
	"sync_first\t1\n" +
	"use_lazy_deletes\t1\n" +
	"retain\tdaily\t3\n" +
	"retain\tweekly\t2\n" +
	"retain\tmonthly\t2\n" +
	"backup\troot@gc01srvr:/etc/\tgc01srvr/\n" +
	"backup_script\t/usr/local/bin/dump_db.sh\tdb/\n"

// Legacy "interval" keyword variant.
const legacyConf = "snapshot_root\t/backup/snapshots\n" +
	"interval\thourly\t6\n" +
	"interval\tdaily\t7\n"

func TestParseRsnapshotConf(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want rsnapConf
	}{
		{
			name: "gc01 main conf",
			in:   gc01Conf,
			want: rsnapConf{
				SnapshotRoot: "/media/backup/rsnapshot",
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
		},
		{
			name: "gc03 gc01srvr conf",
			in:   gc03Gc01srvrConf,
			want: rsnapConf{
				SnapshotRoot: "/media/backup/rsnapshot-gc01srvr",
				Lockfile:     "/var/run/rsnapshot-gc01srvr.pid",
				Logfile:      "/var/log/rsnapshot-gc01srvr.log",
				SyncFirst:    true,
				LazyDeletes:  true,
				Intervals: []rsnapInterval{
					{Name: "daily", Keep: 3},
					{Name: "weekly", Keep: 2},
					{Name: "monthly", Keep: 2},
				},
				BackupLines: 2,
			},
		},
		{
			name: "legacy interval keyword",
			in:   legacyConf,
			want: rsnapConf{
				SnapshotRoot: "/backup/snapshots",
				Intervals: []rsnapInterval{
					{Name: "hourly", Keep: 6},
					{Name: "daily", Keep: 7},
				},
			},
		},
		{
			name: "empty input",
			in:   "",
			want: rsnapConf{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRsnapshotConf(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestParseRsnapshotConfSpaceFallback(t *testing.T) {
	in := "snapshot_root /media/backup/rsnapshot/\n" +
		"retain hoursago 6\n" +
		"retain daysago 7\n"
	got := parseRsnapshotConf(in)
	if got.SnapshotRoot != "/media/backup/rsnapshot" {
		t.Fatalf("root: %q", got.SnapshotRoot)
	}
	want := []rsnapInterval{{Name: "hoursago", Keep: 6}, {Name: "daysago", Keep: 7}}
	if !reflect.DeepEqual(got.Intervals, want) {
		t.Fatalf("intervals: %+v", got.Intervals)
	}
	if len(got.Problems) != 1 {
		t.Fatalf("want exactly one fallback note, got %v", got.Problems)
	}
}

func TestParseRsnapshotConfProblems(t *testing.T) {
	in := "snapshot_root\t/backup/\n" +
		"retain\thoursago\tsix\n" +
		"include_conf\t/etc/rsnapshot-extra.conf\n"
	got := parseRsnapshotConf(in)
	if len(got.Intervals) != 0 {
		t.Fatalf("unparseable count must skip the line: %+v", got.Intervals)
	}
	// The unparseable retain count is fatal; include_conf is a non-fatal note.
	if len(got.Problems) != 1 || !strings.Contains(got.Problems[0], "retain") {
		t.Fatalf("problems: %v", got.Problems)
	}
	if len(got.Notes) != 1 || !strings.Contains(got.Notes[0], "include_conf") {
		t.Fatalf("notes: %v", got.Notes)
	}
}

func TestParseRsnapshotConfMixedSeparators(t *testing.T) {
	// A stray tab on a space-separated retain line must not silently drop the
	// directive: the interval registers in file order and the tab requirement
	// is flagged like a pure-space line.
	cases := []struct {
		name string
		in   string
	}{
		{"trailing tab", "retain hoursago 6\t\nretain\tdaysago\t7\n"},
		{"space then tab", "retain hoursago\t6\nretain\tdaysago\t7\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRsnapshotConf(tc.in)
			want := []rsnapInterval{{Name: "hoursago", Keep: 6}, {Name: "daysago", Keep: 7}}
			if !reflect.DeepEqual(got.Intervals, want) {
				t.Fatalf("intervals: %+v", got.Intervals)
			}
			if len(got.Problems) != 1 || !strings.Contains(got.Problems[0], "tabs") {
				t.Fatalf("problems: %v", got.Problems)
			}
		})
	}
}
