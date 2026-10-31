// rsnapshot log tail classification: banner-driven result of the last run.
// Pure: fed the tail as a string, no IO.

package collector

import (
	"strings"
	"time"
)

// rsnapLogState summarizes the last run seen in a log tail.
type rsnapLogState struct {
	LastInterval string
	LastResult   string // "success" | "warnings" | "errors" | "incomplete" | "unknown"
	BenignOnly   bool
	LastStart    time.Time
	LastEnd      time.Time
	// LastActivity is the newest timestamp inside that block. Groups share one
	// log file, so the file's mtime says nothing about this config.
	LastActivity time.Time
}

// rsnapLogTimeLayouts are the banner timestamp forms: [2026-10-31T04:19:37]
// from rsnapshot 1.5.x, [10/Jul/2026:05:20:01] from older releases. Banners
// carry no zone, so they are read in the host's local zone.
var rsnapLogTimeLayouts = []string{"2006-01-02T15:04:05", "02/Jan/2006:15:04:05"}

// rsnapCompletions maps completion banner suffixes to results, most common first.
var rsnapCompletions = []struct{ suffix, result string }{
	{": completed successfully", "success"},
	{": completed, but with some warnings", "warnings"},
	{": completed, but with some errors", "errors"},
}

// rsnapLogBlock is one run block: opened by a "<interval>: started" banner,
// closed by a completion banner for the same interval.
type rsnapLogBlock struct {
	conf       string
	interval   string
	start      time.Time
	end        time.Time
	closed     bool
	result     string
	hasError   bool
	warnTotal  int
	warnBenign int
	lastSeen   time.Time
}

// rsnapDefaultConf is the config rsnapshot uses when it is run without -c.
const rsnapDefaultConf = "/etc/rsnapshot.conf"

// rsnapBannerConf returns the config path a banner names with -c, or
// rsnapshot's default config when it names none.
func rsnapBannerConf(content string) string {
	f := strings.Fields(content)
	for i := 0; i+1 < len(f); i++ {
		if f[i] == "-c" {
			return f[i+1]
		}
	}
	return rsnapDefaultConf
}

// rsnapLogLine splits a log line into its banner timestamp (zero when absent
// or unparseable) and the remaining content.
func rsnapLogLine(line string) (time.Time, string) {
	if strings.HasPrefix(line, "[") {
		if i := strings.Index(line, "]"); i > 0 {
			rest := strings.TrimSpace(line[i+1:])
			for _, layout := range rsnapLogTimeLayouts {
				if ts, err := time.ParseInLocation(layout, line[1:i], time.Local); err == nil {
					return ts, rest
				}
			}
			return time.Time{}, rest
		}
	}
	return time.Time{}, strings.TrimSpace(line)
}

// rsnapBenignWarning reports whether a WARNING line is the harmless rsync
// code-24 "files vanished during transfer" case.
func rsnapBenignWarning(content string) bool {
	l := strings.ToLower(content)
	return strings.Contains(l, "vanished") || strings.Contains(l, "code 24")
}

// classifyRsnapshotLog classifies the tail of an rsnapshot log for one config.
// The tail may start mid-line: a partial first line (no leading "[") is
// dropped. Several configs may share one log file, so blocks are attributed to
// the config their started banner names with -c (no -c means the default
// config), and the state reported is that of the last block for confPath whose
// started banner appears in the tail. Other lines belong to the newest open
// block of any config: runs on these hosts are serialised by a shared flock,
// so blocks never interleave across configs. No completion banner means
// "incomplete", no started banner for confPath at all means "unknown".
func classifyRsnapshotLog(tail, confPath string) rsnapLogState {
	if tail != "" && !strings.HasPrefix(tail, "[") {
		if i := strings.Index(tail, "\n"); i >= 0 {
			tail = tail[i+1:]
		} else {
			tail = ""
		}
	}
	var blocks []*rsnapLogBlock
	lastOpen := func() *rsnapLogBlock {
		for i := len(blocks) - 1; i >= 0; i-- {
			if !blocks[i].closed {
				return blocks[i]
			}
		}
		return nil
	}
	seen := func(b *rsnapLogBlock, ts time.Time) {
		if b != nil && ts.After(b.lastSeen) {
			b.lastSeen = ts
		}
	}
	for _, raw := range strings.Split(tail, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		ts, content := rsnapLogLine(line)
		if strings.HasSuffix(content, ": started") {
			head := strings.TrimSuffix(content, ": started")
			f := strings.Fields(head)
			if len(f) > 0 {
				blocks = append(blocks, &rsnapLogBlock{conf: rsnapBannerConf(head), interval: f[len(f)-1], start: ts, lastSeen: ts})
			}
			continue
		}
		completed := false
		for _, c := range rsnapCompletions {
			if !strings.HasSuffix(content, c.suffix) {
				continue
			}
			completed = true
			head := strings.TrimSuffix(content, c.suffix)
			f := strings.Fields(head)
			if len(f) == 0 {
				break
			}
			// Close the most recent open block for this config and interval.
			// Orphan completions (start scrolled out of the tail) are ignored.
			conf := rsnapBannerConf(head)
			for i := len(blocks) - 1; i >= 0; i-- {
				b := blocks[i]
				if !b.closed && b.conf == conf && b.interval == f[len(f)-1] {
					b.closed, b.result, b.end = true, c.result, ts
					seen(b, ts)
					break
				}
			}
			break
		}
		if completed {
			continue
		}
		b := lastOpen()
		seen(b, ts)
		if strings.Contains(content, "ERROR:") {
			if b != nil {
				b.hasError = true
			}
			continue
		}
		if strings.Contains(content, "WARNING") {
			if b != nil {
				b.warnTotal++
				if rsnapBenignWarning(content) {
					b.warnBenign++
				}
			}
		}
	}
	var b *rsnapLogBlock
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].conf == confPath {
			b = blocks[i]
			break
		}
	}
	if b == nil {
		return rsnapLogState{LastResult: "unknown"}
	}
	st := rsnapLogState{
		LastInterval: b.interval,
		LastStart:    b.start,
		LastActivity: b.lastSeen,
		BenignOnly:   b.warnTotal > 0 && b.warnBenign == b.warnTotal,
	}
	switch {
	case !b.closed:
		st.LastResult = "incomplete"
	case b.hasError:
		st.LastResult = "errors"
		st.LastEnd = b.end
	default:
		st.LastResult = b.result
		st.LastEnd = b.end
	}
	return st
}
