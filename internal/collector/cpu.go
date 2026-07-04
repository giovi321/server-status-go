package collector

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/giovi321/server-status/internal/model"
)

// CPUSample is a point-in-time reading of aggregate CPU jiffies.
type CPUSample struct {
	Idle  uint64
	Total uint64
}

// parseCPUSample parses the aggregate "cpu " line of /proc/stat.
func parseCPUSample(line string) (CPUSample, bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return CPUSample{}, false
	}
	var vals []uint64
	for _, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return CPUSample{}, false
		}
		vals = append(vals, v)
	}
	// vals: user nice system idle iowait irq softirq [steal ...]
	idle := vals[3]
	iowait := uint64(0)
	if len(vals) > 4 {
		iowait = vals[4]
	}
	var total uint64
	for _, v := range vals {
		total += v
	}
	return CPUSample{Idle: idle + iowait, Total: total}, true
}

func usagePercent(a, b CPUSample) float64 {
	dTotal := float64(b.Total - a.Total)
	if dTotal <= 0 {
		return 0
	}
	dIdle := float64(b.Idle - a.Idle)
	busy := (dTotal - dIdle) * 100.0 / dTotal
	if busy < 0 {
		return 0
	}
	if busy > 100 {
		return 100
	}
	return busy
}

func readCPUSample() (CPUSample, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return CPUSample{}, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu ") {
			return parseCPUSample(line)
		}
	}
	return CPUSample{}, false
}

// CPU publishes overall CPU usage as a percentage.
type CPU struct{}

func (CPU) Name() string { return "cpu" }

func (CPU) Available() bool {
	_, ok := readCPUSample()
	return ok
}

func (CPU) Collect(ctx context.Context) ([]model.Metric, error) {
	a, ok := readCPUSample()
	if !ok {
		return nil, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Second):
	}
	b, ok := readCPUSample()
	if !ok {
		return nil, nil
	}
	usage := usagePercent(a, b)
	return []model.Metric{{
		Key:        "cpu_usage",
		Name:       "CPU usage",
		Value:      int(usage + 0.5),
		Unit:       "%",
		StateClass: "measurement",
		Kind:       model.KindSensor,
		Category:   "primary",
		Icon:       "mdi:cpu-64-bit",
	}}, nil
}
