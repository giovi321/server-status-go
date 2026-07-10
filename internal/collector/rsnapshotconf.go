// rsnapshot.conf parsing: retain intervals, snapshot root, lock/log paths, and
// conf-level problems. Pure: fed the file contents as a string, no IO.

package collector

import (
	"strconv"
	"strings"
)

// rsnapInterval is one retain level. File order is frequency order: first = most frequent.
type rsnapInterval struct {
	Name string
	Keep int
}

// rsnapConf is the monitoring-relevant view of one rsnapshot config file.
// Problems are faults rsnapshot itself would reject the file for (fatal);
// Notes are non-fatal observations about monitoring coverage.
type rsnapConf struct {
	Path         string
	SnapshotRoot string
	Lockfile     string
	Logfile      string
	SyncFirst    bool
	LazyDeletes  bool
	Intervals    []rsnapInterval
	BackupLines  int
	Problems     []string
	Notes        []string
}

// parseRsnapshotConf parses one rsnapshot config file. Fields are TAB-separated
// per rsnapshot convention; lines without tabs fall back to whitespace fields
// (noted once in Problems). Path is set by the caller, not here.
func parseRsnapshotConf(data string) rsnapConf {
	var c rsnapConf
	spaceNoted := false
	noteSpace := func() {
		if !spaceNoted {
			c.Problems = append(c.Problems, "space-separated directives (rsnapshot requires tabs)")
			spaceNoted = true
		}
	}
	for _, line := range strings.Split(data, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		var f []string
		if strings.Contains(line, "\t") {
			for _, part := range strings.Split(line, "\t") {
				if p := strings.TrimSpace(part); p != "" {
					f = append(f, p)
				}
			}
			// Mixed separators: rsnapshot splits on tabs only and would
			// reject this line. Reparse on whitespace so the directive still
			// registers in file order, and flag it like a pure-space line.
			if len(f) > 0 && strings.Contains(f[0], " ") {
				f = strings.Fields(trimmed)
				noteSpace()
			}
		} else {
			f = strings.Fields(trimmed)
			if len(f) >= 2 {
				noteSpace()
			}
		}
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "retain", "interval":
			if len(f) < 3 {
				c.Problems = append(c.Problems, "unparseable "+f[0]+" line: "+trimmed)
				continue
			}
			keep, err := strconv.Atoi(f[2])
			if err != nil {
				c.Problems = append(c.Problems, "unparseable "+f[0]+" count: "+f[2])
				continue
			}
			c.Intervals = append(c.Intervals, rsnapInterval{Name: f[1], Keep: keep})
		case "snapshot_root":
			if len(f) >= 2 {
				root := f[1]
				for len(root) > 1 && strings.HasSuffix(root, "/") {
					root = root[:len(root)-1]
				}
				c.SnapshotRoot = root
			}
		case "lockfile":
			if len(f) >= 2 {
				c.Lockfile = f[1]
			}
		case "logfile":
			if len(f) >= 2 {
				c.Logfile = f[1]
			}
		case "sync_first":
			c.SyncFirst = len(f) >= 2 && f[1] == "1"
		case "use_lazy_deletes":
			c.LazyDeletes = len(f) >= 2 && f[1] == "1"
		case "backup", "backup_script":
			c.BackupLines++
		case "include_conf":
			// Valid rsnapshot syntax; we just do not read the included file.
			c.Notes = append(c.Notes, "include_conf not followed")
		}
	}
	return c
}
