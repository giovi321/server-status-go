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
}

// rsnapLogTimeLayout matches the [10/Jul/2026:05:20:01] banner timestamps.
const rsnapLogTimeLayout = "02/Jan/2006:15:04:05"

// rsnapCompletions maps completion banner suffixes to results, most common first.
var rsnapCompletions = []struct{ suffix, result string }{
	{": completed successfully", "success"},
	{": completed, but with some warnings", "warnings"},
	{": completed, but with some errors", "errors"},
}

// rsnapLogBlock is one run block: opened by a "<interval>: started" banner,
// closed by a completion banner for the same interval.
type rsnapLogBlock struct {
	interval   string
	start      time.Time
	end        time.Time
	closed     bool
	result     string
	hasError   bool
	warnTotal  int
	warnBenign int
}

// rsnapLogLine splits a log line into its banner timestamp (zero when absent
// or unparseable) and the remaining content.
func rsnapLogLine(line string) (time.Time, string) {
	if strings.HasPrefix(line, "[") {
		if i := strings.Index(line, "]"); i > 0 {
			rest := strings.TrimSpace(line[i+1:])
			if ts, err := time.Parse(rsnapLogTimeLayout, line[1:i]); err == nil {
				return ts, rest
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

// classifyRsnapshotLog classifies the tail of an rsnapshot log. The tail may
// start mid-line: a partial first line (no leading "[") is dropped. The state
// reported is that of the last block whose started banner appears in the tail;
// no completion banner for it means "incomplete", no started banner at all
// means "unknown".
func classifyRsnapshotLog(tail string) rsnapLogState {
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
	for _, raw := range strings.Split(tail, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		ts, content := rsnapLogLine(line)
		if strings.HasSuffix(content, ": started") {
			f := strings.Fields(strings.TrimSuffix(content, ": started"))
			if len(f) > 0 {
				blocks = append(blocks, &rsnapLogBlock{interval: f[len(f)-1], start: ts})
			}
			continue
		}
		completed := false
		for _, c := range rsnapCompletions {
			if !strings.HasSuffix(content, c.suffix) {
				continue
			}
			completed = true
			f := strings.Fields(strings.TrimSuffix(content, c.suffix))
			if len(f) == 0 {
				break
			}
			// Close the most recent open block for this interval. Orphan
			// completions (start scrolled out of the tail) are ignored.
			for i := len(blocks) - 1; i >= 0; i-- {
				b := blocks[i]
				if !b.closed && b.interval == f[len(f)-1] {
					b.closed, b.result, b.end = true, c.result, ts
					break
				}
			}
			break
		}
		if completed {
			continue
		}
		if strings.Contains(content, "ERROR:") {
			if b := lastOpen(); b != nil {
				b.hasError = true
			}
			continue
		}
		if strings.Contains(content, "WARNING") {
			if b := lastOpen(); b != nil {
				b.warnTotal++
				if rsnapBenignWarning(content) {
					b.warnBenign++
				}
			}
		}
	}
	if len(blocks) == 0 {
		return rsnapLogState{LastResult: "unknown"}
	}
	b := blocks[len(blocks)-1]
	st := rsnapLogState{
		LastInterval: b.interval,
		LastStart:    b.start,
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
