package collector

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// RaidArray is one mdadm array parsed from /proc/mdstat.
type RaidArray struct {
	Name, Level, State string
	Active, Total      int
	Failed             int
	ResyncPct          int
	ResyncActive       bool
}

var (
	mdHeader   = regexp.MustCompile(`^(md\d+)\s*:\s*(\S+)(?:\s+(\S+))?`)
	mdCounts   = regexp.MustCompile(`\[(\d+)/(\d+)\]`)
	mdRecovery = regexp.MustCompile(`(recovery|resync|reshape|check)\s*=\s*(\d+)(?:\.\d+)?%`)
)

// parseMdstat parses /proc/mdstat into a slice of arrays.
func parseMdstat(data string) []RaidArray {
	var arrays []RaidArray
	var cur *RaidArray
	flush := func() {
		if cur != nil {
			arrays = append(arrays, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(data, "\n") {
		if m := mdHeader.FindStringSubmatch(line); m != nil {
			flush()
			cur = &RaidArray{Name: m[1], State: m[2], Level: m[3]}
			continue
		}
		if cur == nil {
			continue
		}
		if m := mdCounts.FindStringSubmatch(line); m != nil {
			cur.Total, _ = strconv.Atoi(m[1])
			cur.Active, _ = strconv.Atoi(m[2])
			if cur.Total >= cur.Active {
				cur.Failed = cur.Total - cur.Active
			}
		}
		if m := mdRecovery.FindStringSubmatch(line); m != nil {
			cur.ResyncActive = true
			cur.ResyncPct, _ = strconv.Atoi(m[2])
		}
	}
	flush()
	return arrays
}

func raidMetrics(a RaidArray) []model.Metric {
	comp := "raid-" + a.Name
	name := "RAID " + a.Name
	m := func(key, leaf string, val any, kind model.Kind, dc, cat string) model.Metric {
		return model.Metric{Key: key, Component: comp, ComponentName: name, Name: leaf, Value: val, Kind: kind, DeviceClass: dc, Category: cat}
	}
	degraded := a.Failed > 0 || (a.State != "active" && a.State != "clean")
	out := []model.Metric{
		m("raid_state", "State", a.State, model.KindText, "", "primary"),
		m("raid_degraded", "Degraded", degraded, model.KindBinarySensor, "problem", "primary"),
		{Key: "raid_active_devices", Component: comp, ComponentName: name, Name: "Active devices", Value: a.Active, StateClass: "measurement", Kind: model.KindSensor, Category: "primary"},
		{Key: "raid_total_devices", Component: comp, ComponentName: name, Name: "Total devices", Value: a.Total, StateClass: "measurement", Kind: model.KindSensor, Category: "diagnostic"},
		{Key: "raid_failed_devices", Component: comp, ComponentName: name, Name: "Failed devices", Value: a.Failed, StateClass: "measurement", Kind: model.KindSensor, Category: "primary"},
	}
	if a.ResyncActive {
		out = append(out, model.Metric{Key: "raid_resync_progress", Component: comp, ComponentName: name, Name: "Resync progress", Value: a.ResyncPct, Unit: "%", StateClass: "measurement", Kind: model.KindSensor, Category: "primary"})
	}
	return out
}

func readMdstat() []RaidArray {
	data, err := os.ReadFile("/proc/mdstat")
	if err != nil {
		return nil
	}
	return parseMdstat(string(data))
}

// Mdadm publishes mdadm RAID array health as sub-devices.
type Mdadm struct{}

func (Mdadm) Name() string { return "mdadm" }

func (Mdadm) Available() bool { return len(readMdstat()) > 0 }

func (Mdadm) Collect(ctx context.Context) ([]model.Metric, error) {
	var out []model.Metric
	for _, a := range readMdstat() {
		out = append(out, raidMetrics(a)...)
	}
	return out, nil
}
