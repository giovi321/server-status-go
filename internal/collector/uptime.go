package collector

import (
	"context"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// parseUptime reads the first field of /proc/uptime (seconds) and returns days.
func parseUptime(data string) (float64, bool) {
	fields := strings.Fields(data)
	if len(fields) == 0 {
		return 0, false
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return secs / 86400.0, true
}

func uptimeMetric(days float64) model.Metric {
	var value any
	if days < 10 {
		value = math.Round(days*100) / 100
	} else {
		value = int(days + 0.5)
	}
	return model.Metric{
		Key:        "uptime",
		Name:       "Uptime",
		Value:      value,
		Unit:       "d",
		StateClass: "measurement",
		Kind:       model.KindSensor,
		Category:   "primary",
		Icon:       "mdi:clock-outline",
	}
}

// Uptime publishes system uptime in days.
type Uptime struct{}

func (Uptime) Name() string { return "uptime" }

func (Uptime) Available() bool {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return false
	}
	_, ok := parseUptime(string(data))
	return ok
}

func (Uptime) Collect(ctx context.Context) ([]model.Metric, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return nil, nil
	}
	days, ok := parseUptime(string(data))
	if !ok {
		return nil, nil
	}
	return []model.Metric{uptimeMetric(days)}, nil
}
