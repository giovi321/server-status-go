// crontab parsing and schedule bounds for rsnapshot jobs: which entries drive
// which config/interval, and the maximum gap between firings of a spec.

package collector

import (
	"strconv"
	"strings"
	"time"
)

// cronEntry is one command line from a crontab.
type cronEntry struct {
	Spec    string
	Command string
	Source  string
}

// parseCronFile parses a crontab; systemFormat crontabs carry a user column.
func parseCronFile(data string, systemFormat bool, source string) []cronEntry {
	var entries []cronEntry
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if strings.Contains(fields[0], "=") || (len(fields) > 1 && strings.HasPrefix(fields[1], "=")) {
			continue // NAME=value environment line
		}
		specFields := 5
		if strings.HasPrefix(line, "@") {
			specFields = 1
		}
		skip := specFields
		if systemFormat {
			skip++ // user column
		}
		head, command, ok := cronSplitFields(line, skip)
		if !ok || command == "" {
			continue // malformed
		}
		entries = append(entries, cronEntry{
			Spec:    strings.Join(head[:specFields], " "),
			Command: command,
			Source:  source,
		})
	}
	return entries
}

// cronSplitFields consumes n whitespace-delimited fields and returns them plus
// the remainder of the line with leading whitespace stripped.
func cronSplitFields(line string, n int) ([]string, string, bool) {
	fields := make([]string, 0, n)
	rest := line
	for i := 0; i < n; i++ {
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			return nil, "", false
		}
		if j := strings.IndexAny(rest, " \t"); j < 0 {
			fields = append(fields, rest)
			rest = ""
		} else {
			fields = append(fields, rest[:j])
			rest = rest[j:]
		}
	}
	return fields, strings.TrimLeft(rest, " \t"), true
}

// matchRsnapshotCron attributes entries to a config's intervals by conf path
// and retain name; isDefaultConf claims rsnapshot commands with no conf path.
// otherConfPaths lists the other monitored configs so their cron lines never
// leak onto the default conf, even when they live outside /etc/rsnapshot*.
func matchRsnapshotCron(entries []cronEntry, confPath string, isDefaultConf bool, otherConfPaths []string, intervals []string) map[string][]cronEntry {
	matches := make(map[string][]cronEntry)
	for _, e := range entries {
		if !strings.Contains(e.Command, "rsnapshot") {
			continue
		}
		switch {
		case strings.Contains(e.Command, confPath):
			// names this config explicitly
		case cronNamesOther(e.Command, otherConfPaths):
			continue // names another monitored config
		case strings.Contains(e.Command, "/etc/rsnapshot"):
			continue // names a different config
		case !isDefaultConf:
			continue // pathless rsnapshot commands belong to the default conf
		}
		tokens := strings.Fields(e.Command)
		for _, name := range intervals {
			for _, tok := range tokens {
				if tok == name {
					matches[name] = append(matches[name], e)
					break
				}
			}
		}
	}
	return matches
}

// cronNamesOther reports whether a cron command mentions any of the given
// conf paths.
func cronNamesOther(cmd string, paths []string) bool {
	for _, p := range paths {
		if p != "" && strings.Contains(cmd, p) {
			return true
		}
	}
	return false
}

// cronAliases are the @-shorthands that expand to a 5-field spec. @reboot has
// no schedule and stays unexpandable.
var cronAliases = map[string]string{
	"@hourly":   "0 * * * *",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@weekly":   "0 0 * * 0",
	"@monthly":  "0 0 1 * *",
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
}

var cronMonthNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

var cronDowNames = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

// cronValue resolves one field value: a number or a lowercase name.
func cronValue(s string, names map[string]int) (int, bool) {
	if v, err := strconv.Atoi(s); err == nil {
		return v, true
	}
	v, ok := names[strings.ToLower(s)]
	return v, ok
}

// parseCronField expands one cron field (lists, ranges, steps, names) into a
// value set indexed 0..hi. restricted reports whether the field constrains the
// schedule (does not start with *), per the vixie dom/dow OR-rule.
func parseCronField(field string, lo, hi int, names map[string]int) (set []bool, restricted bool, ok bool) {
	if field == "" {
		return nil, false, false
	}
	set = make([]bool, hi+1)
	restricted = field[0] != '*'
	for _, part := range strings.Split(field, ",") {
		base, stepStr, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepStr)
			if err != nil || n <= 0 {
				return nil, false, false
			}
			step = n
		}
		var from, to int
		switch {
		case base == "*":
			from, to = lo, hi
		case strings.Contains(base, "-"):
			a, b, _ := strings.Cut(base, "-")
			var oka, okb bool
			from, oka = cronValue(a, names)
			to, okb = cronValue(b, names)
			if !oka || !okb {
				return nil, false, false
			}
		default:
			v, okv := cronValue(base, names)
			if !okv {
				return nil, false, false
			}
			from, to = v, v
			if hasStep {
				to = hi // vixie: value/step runs from value to max
			}
		}
		if from < lo || to > hi || from > to {
			return nil, false, false
		}
		for v := from; v <= to; v += step {
			set[v] = true
		}
	}
	return set, restricted, true
}

// cronMaxGap returns the maximum gap between consecutive firings of a cron spec.
func cronMaxGap(spec string) (time.Duration, bool) {
	spec = strings.TrimSpace(spec)
	if strings.HasPrefix(spec, "@") {
		expanded, ok := cronAliases[strings.ToLower(spec)]
		if !ok {
			return 0, false // @reboot and unknown aliases have no period
		}
		spec = expanded
	}
	f := strings.Fields(spec)
	if len(f) != 5 {
		return 0, false
	}
	minutes, _, ok1 := parseCronField(f[0], 0, 59, nil)
	hours, _, ok2 := parseCronField(f[1], 0, 23, nil)
	dom, domRestricted, ok3 := parseCronField(f[2], 1, 31, nil)
	months, _, ok4 := parseCronField(f[3], 1, 12, cronMonthNames)
	dow, dowRestricted, ok5 := parseCronField(f[4], 0, 7, cronDowNames)
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
		return 0, false
	}
	if dow[7] {
		dow[0] = true // 7 == 0 == Sunday
	}
	// Simulate firings over a fixed 400-day horizon and track the widest gap.
	const horizonDays = 400
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var prev time.Time
	var maxGap time.Duration
	firings := 0
	for d := 0; d < horizonDays; d++ {
		day := epoch.AddDate(0, 0, d)
		if !months[int(day.Month())] {
			continue
		}
		// Vixie day rule: the OR applies only when neither field starts with
		// *. A star-with-step field (*/2) keeps its star flag but its value
		// set must still match, so everything else is an AND (a plain * has
		// a full set, making the AND a no-op).
		var dayMatch bool
		if domRestricted && dowRestricted {
			dayMatch = dom[day.Day()] || dow[int(day.Weekday())]
		} else {
			dayMatch = dom[day.Day()] && dow[int(day.Weekday())]
		}
		if !dayMatch {
			continue
		}
		for h := 0; h < 24; h++ {
			if !hours[h] {
				continue
			}
			for m := 0; m < 60; m++ {
				if !minutes[m] {
					continue
				}
				t := day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
				if firings > 0 && t.Sub(prev) > maxGap {
					maxGap = t.Sub(prev)
				}
				prev = t
				firings++
			}
		}
	}
	if firings < 2 {
		return 0, false
	}
	return maxGap, true
}

// rsnapshotCronBound derives a staleness bound from matched entries; root
// crontab entries are authoritative over other sources.
func rsnapshotCronBound(matches []cronEntry) (time.Duration, bool) {
	chosen := matches
	var root []cronEntry
	for _, e := range matches {
		if e.Source == "/var/spool/cron/crontabs/root" || e.Source == "/var/spool/cron/root" {
			root = append(root, e)
		}
	}
	if len(root) > 0 {
		chosen = root
	}
	var bound time.Duration
	found := false
	for _, e := range chosen {
		g, ok := cronMaxGap(e.Spec)
		if !ok {
			continue
		}
		if !found || g < bound {
			bound, found = g, true
		}
	}
	return bound, found
}
