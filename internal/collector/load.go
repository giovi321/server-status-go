package collector

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// parseLoadAvg parses the first three fields of /proc/loadavg.
func parseLoadAvg(data string) ([3]float64, bool) {
	f := strings.Fields(data)
	if len(f) < 3 {
		return [3]float64{}, false
	}
	var out [3]float64
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseFloat(f[i], 64)
		if err != nil {
			return [3]float64{}, false
		}
		out[i] = v
	}
	return out, true
}

func loadMetrics(v [3]float64) []model.Metric {
	mk := func(key, name string, val float64) model.Metric {
		return model.Metric{Key: key, Name: name, Value: val, StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:gauge"}
	}
	return []model.Metric{
		mk("load_1m", "Load 1m", v[0]),
		mk("load_5m", "Load 5m", v[1]),
		mk("load_15m", "Load 15m", v[2]),
	}
}

// Load publishes the 1/5/15-minute load averages.
type Load struct{}

func (Load) Name() string { return "load" }

func (Load) Available() bool {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return false
	}
	_, ok := parseLoadAvg(string(data))
	return ok
}

func (Load) Collect(ctx context.Context) ([]model.Metric, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil, nil
	}
	v, ok := parseLoadAvg(string(data))
	if !ok {
		return nil, nil
	}
	return loadMetrics(v), nil
}
