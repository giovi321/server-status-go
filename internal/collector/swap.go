package collector

import (
	"context"
	"os"

	"github.com/giovi321/server-status/internal/model"
)

func swapMetric(mem map[string]uint64) (model.Metric, bool) {
	total := mem["SwapTotal"]
	if total == 0 {
		return model.Metric{}, false
	}
	used := total - mem["SwapFree"]
	return model.Metric{
		Key: "swap_used", Name: "Swap used", Value: pct(used, total), Unit: "%",
		StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:harddisk",
	}, true
}

// Swap publishes swap used percentage, only when swap is configured.
type Swap struct{}

func (Swap) Name() string { return "swap" }

func (Swap) Available() bool {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return false
	}
	mem, ok := parseMeminfo(string(data))
	if !ok {
		return false
	}
	_, has := swapMetric(mem)
	return has
}

func (Swap) Collect(ctx context.Context) ([]model.Metric, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, nil
	}
	mem, ok := parseMeminfo(string(data))
	if !ok {
		return nil, nil
	}
	m, has := swapMetric(mem)
	if !has {
		return nil, nil
	}
	return []model.Metric{m}, nil
}
