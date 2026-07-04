package collector

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// parseMeminfo parses /proc/meminfo into a map of key -> kB value.
func parseMeminfo(data string) (map[string]uint64, bool) {
	out := map[string]uint64{}
	for _, line := range strings.Split(data, "\n") {
		key, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		if v, err := strconv.ParseUint(fields[0], 10, 64); err == nil {
			out[key] = v
		}
	}
	_, ok := out["MemTotal"]
	return out, ok
}

func pct(part, total uint64) int {
	if total == 0 {
		return 0
	}
	return int(float64(part)*100.0/float64(total) + 0.5)
}

func memoryMetrics(mem map[string]uint64) []model.Metric {
	total := mem["MemTotal"]
	avail := mem["MemAvailable"]
	used := total - avail
	return []model.Metric{
		{Key: "memory_used", Name: "Memory used", Value: pct(used, total), Unit: "%", StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:memory"},
		{Key: "memory_available", Name: "Memory available", Value: pct(avail, total), Unit: "%", StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:memory"},
	}
}

// Memory publishes memory used and available percentages.
type Memory struct{}

func (Memory) Name() string { return "memory" }

func (Memory) Available() bool {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return false
	}
	_, ok := parseMeminfo(string(data))
	return ok
}

func (Memory) Collect(ctx context.Context) ([]model.Metric, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, nil
	}
	mem, ok := parseMeminfo(string(data))
	if !ok {
		return nil, nil
	}
	return memoryMetrics(mem), nil
}
