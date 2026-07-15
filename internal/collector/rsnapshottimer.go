// systemd .timer parsing and schedule bounds for rsnapshot jobs — the successor
// to the crontab source. Which enabled timers drive which config/interval, and
// the maximum gap between firings of an OnCalendar spec. Everything comes from
// reading unit files under the standard systemd directories; nothing is executed.

package collector

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// systemdUnitDirs are the standard locations a unit file can live, most-
// authoritative first (etc overrides run overrides vendor).
var systemdUnitDirs = []string{
	"/etc/systemd/system",
	"/run/systemd/system",
	"/usr/lib/systemd/system",
	"/lib/systemd/system",
}

// timerUnit is one .timer relevant to rsnapshot, with its activated service's
// config/interval already resolved so matching stays pure.
type timerUnit struct {
	Name      string   // e.g. "rsnapshot-hoursago.timer"
	Calendars []string // OnCalendar= values
	Activates string   // the .service it starts (Unit=, or <name>.service)
	ConfPath  string   // conf path from the activated service's ExecStart, "" if unresolved
	Interval  string   // instance name of the activated service (the rsnapshot interval)
	Enabled   bool     // has a timers.target.wants symlink
	Source    string   // unit file path
}

// readTimerUnits reads rsnapshot-related .timer units from the systemd dirs and
// resolves each one's activated service, interval, config and enabled state.
// readable means at least one systemd dir could be listed; absent dirs are normal.
func readTimerUnits() ([]timerUnit, bool) {
	seen := map[string]bool{}
	var units []timerUnit
	readable := false
	for _, dir := range systemdUnitDirs {
		dents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		readable = true
		for _, d := range dents {
			name := d.Name()
			if !strings.HasSuffix(name, ".timer") || seen[name] {
				continue
			}
			seen[name] = true // higher-priority dir wins
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			cals, activates := parseTimerUnit(string(data))
			if activates == "" {
				activates = strings.TrimSuffix(name, ".timer") + ".service"
			}
			// only rsnapshot jobs are of interest; skip reading unrelated services
			if !strings.Contains(name, "rsnapshot") && !strings.Contains(activates, "rsnapshot") {
				continue
			}
			units = append(units, timerUnit{
				Name:      name,
				Calendars: cals,
				Activates: activates,
				ConfPath:  serviceConfPath(activates),
				Interval:  instanceName(activates),
				Enabled:   timerEnabled(name),
				Source:    filepath.Join(dir, name),
			})
		}
	}
	return units, readable
}

// parseTimerUnit extracts the [Timer] section's OnCalendar values (multiple
// allowed) and Unit= (the activated service).
func parseTimerUnit(data string) (calendars []string, unit string) {
	inTimer := false
	for _, line := range strings.Split(data, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") {
			continue
		}
		if strings.HasPrefix(t, "[") {
			inTimer = t == "[Timer]"
			continue
		}
		if !inTimer {
			continue
		}
		if v, ok := strings.CutPrefix(t, "OnCalendar="); ok {
			if v = strings.TrimSpace(v); v != "" {
				calendars = append(calendars, v)
			}
		} else if v, ok := strings.CutPrefix(t, "Unit="); ok {
			unit = strings.TrimSpace(v)
		}
	}
	return calendars, unit
}

// timerEnabled reports whether a timer has an enable symlink in any systemd
// dir's timers.target.wants (systemctl enable, or a vendor preset).
func timerEnabled(name string) bool {
	for _, dir := range systemdUnitDirs {
		if _, err := os.Lstat(filepath.Join(dir, "timers.target.wants", name)); err == nil {
			return true
		}
	}
	return false
}

// instanceName returns the instance of a templated unit (foo@inst.service ->
// inst), or "" for a plain unit.
func instanceName(unit string) string {
	unit = strings.TrimSuffix(unit, ".service")
	if i := strings.IndexByte(unit, '@'); i >= 0 {
		return unit[i+1:]
	}
	return ""
}

// serviceConfPath reads the activated service's unit file (the template file for
// an instance: foo@inst.service -> foo@.service) and returns the rsnapshot conf
// path from its ExecStart, mirroring how the cron source reads the command.
func serviceConfPath(activates string) string {
	file := activates
	if i := strings.IndexByte(file, '@'); i >= 0 {
		file = file[:i+1] + ".service" // template file
	}
	for _, dir := range systemdUnitDirs {
		if data, err := os.ReadFile(filepath.Join(dir, file)); err == nil {
			return execStartConf(string(data))
		}
	}
	return ""
}

// execStartConf returns the first ".conf" token of the [Service] ExecStart line.
func execStartConf(data string) string {
	inService := false
	for _, line := range strings.Split(data, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			inService = t == "[Service]"
			continue
		}
		if !inService {
			continue
		}
		v, ok := strings.CutPrefix(t, "ExecStart=")
		if !ok {
			continue
		}
		v = strings.TrimPrefix(v, "-") // ExecStart=- (ignore-failure) prefix
		for _, tok := range strings.Fields(v) {
			if strings.HasSuffix(tok, ".conf") {
				return tok
			}
		}
	}
	return ""
}

// matchRsnapshotTimers attributes enabled timers to a config's intervals: a
// timer belongs to this config when its activated service targets confPath
// exactly, or (when the conf could not be resolved) only to the default conf.
func matchRsnapshotTimers(units []timerUnit, confPath string, isDefaultConf bool, intervals []string) map[string][]timerUnit {
	ivset := make(map[string]bool, len(intervals))
	for _, iv := range intervals {
		ivset[iv] = true
	}
	matches := make(map[string][]timerUnit)
	for _, u := range units {
		if !u.Enabled || !strings.Contains(u.Activates, "rsnapshot") {
			continue
		}
		switch {
		case u.ConfPath == confPath:
			// ours
		case u.ConfPath != "":
			continue // targets a specific, different config
		case !isDefaultConf:
			continue // an unresolved conf only defaults to the main config
		}
		if u.Interval == "" || !ivset[u.Interval] {
			continue
		}
		matches[u.Interval] = append(matches[u.Interval], u)
	}
	return matches
}

// onCalendarShorthand maps systemd calendar shorthands to an equivalent
// OnCalendar expression, so the gap math stays in one place (cronMaxGap).
var onCalendarShorthand = map[string]string{
	"minutely":     "*-*-* *:*:00",
	"hourly":       "*-*-* *:00:00",
	"daily":        "*-*-* 00:00:00",
	"weekly":       "Mon *-*-* 00:00:00",
	"monthly":      "*-*-01 00:00:00",
	"quarterly":    "*-01,04,07,10-01 00:00:00",
	"semiannually": "*-01,07-01 00:00:00",
	"yearly":       "*-01-01 00:00:00",
	"annually":     "*-01-01 00:00:00",
}

// onCalendarToCron converts the common OnCalendar shape "[DOW] [DATE] TIME" into
// an equivalent 5-field cron spec, so cronMaxGap can compute the gap. systemd and
// cron share number/list/range/step syntax; the only rewrites are '..' -> '-' and
// mapping weekday names (which cron's parser already lowercases). Returns false
// for anything it cannot map (specific year, sub-minute seconds, missing time).
func onCalendarToCron(spec string) (string, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", false
	}
	var dow, date, tm string
	for _, f := range strings.Fields(spec) {
		switch {
		case strings.Contains(f, ":"):
			tm = f
		case strings.Contains(f, "-"):
			date = f
		default:
			dow = f
		}
	}
	if tm == "" {
		return "", false
	}
	tp := strings.Split(tm, ":")
	if len(tp) < 2 || len(tp) > 3 {
		return "", false
	}
	if len(tp) == 3 && strings.ContainsAny(tp[2], "*/") {
		return "", false // sub-minute firing
	}
	hour := strings.ReplaceAll(tp[0], "..", "-")
	minute := strings.ReplaceAll(tp[1], "..", "-")

	domF, monF := "*", "*"
	if date != "" {
		dp := strings.Split(date, "-")
		if len(dp) != 3 || dp[0] != "*" { // specific year = one-shot
			return "", false
		}
		monF = strings.ReplaceAll(dp[1], "..", "-")
		domF = strings.ReplaceAll(dp[2], "..", "-")
	}
	dowF := "*"
	if dow != "" && dow != "*" {
		dowF = strings.ReplaceAll(dow, "..", "-")
	}
	return strings.Join([]string{minute, hour, domF, monF, dowF}, " "), true
}

// onCalendarMaxGap returns the maximum gap between consecutive firings of an
// OnCalendar spec (shorthand or explicit).
func onCalendarMaxGap(spec string) (time.Duration, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, false
	}
	if expanded, ok := onCalendarShorthand[strings.ToLower(spec)]; ok {
		spec = expanded
	}
	cron, ok := onCalendarToCron(spec)
	if !ok {
		return 0, false
	}
	return cronMaxGap(cron)
}

// rsnapshotTimerBound derives a staleness bound from matched timers: the tightest
// timer wins; within a timer with several OnCalendar values (a union schedule),
// the smallest per-calendar gap is used.
func rsnapshotTimerBound(units []timerUnit) (time.Duration, bool) {
	var bound time.Duration
	found := false
	for _, u := range units {
		var ug time.Duration
		ugFound := false
		for _, c := range u.Calendars {
			g, ok := onCalendarMaxGap(c)
			if !ok {
				continue
			}
			if !ugFound || g < ug {
				ug, ugFound = g, true
			}
		}
		if !ugFound {
			continue
		}
		if !found || ug < bound {
			bound, found = ug, true
		}
	}
	return bound, found
}
