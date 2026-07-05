package collector

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// TempReading is one temperature sensor reading in milli-degrees Celsius.
type TempReading struct {
	Chip   string // hwmon dir, e.g. "hwmon0" — makes the instance unique across identically-named chips
	Sensor string // e.g. "temp1"
	Label  string // human display label
	MilliC int
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// hwmonReadings walks a /sys/class/hwmon-style tree and returns labeled temperature readings.
func hwmonReadings(root string) []TempReading {
	chips, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []TempReading
	for _, chip := range chips {
		dir := filepath.Join(root, chip.Name())
		chipName := readTrim(filepath.Join(dir, "name"))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var inputs []string
		for _, e := range entries {
			n := e.Name()
			if strings.HasPrefix(n, "temp") && strings.HasSuffix(n, "_input") {
				inputs = append(inputs, n)
			}
		}
		sort.Strings(inputs)
		for _, in := range inputs {
			raw := readTrim(filepath.Join(dir, in))
			v, err := strconv.Atoi(raw)
			if err != nil {
				continue
			}
			idx := strings.TrimSuffix(in, "_input") // "temp1"
			label := readTrim(filepath.Join(dir, idx+"_label"))
			if label == "" {
				base := chipName
				if base == "" {
					base = chip.Name()
				}
				label = base + " " + idx
			}
			out = append(out, TempReading{Chip: chip.Name(), Sensor: idx, Label: label, MilliC: v})
		}
	}
	return out
}

func tempMetrics(readings []TempReading) []model.Metric {
	var out []model.Metric
	for _, r := range readings {
		out = append(out, model.Metric{
			Key: "temperature", Instance: r.Chip + "-" + r.Sensor, Name: r.Label + " temperature",
			Value: int(float64(r.MilliC)/1000.0 + 0.5), Unit: "°C", DeviceClass: "temperature",
			StateClass: "measurement", Kind: model.KindSensor, Category: "primary",
		})
	}
	return out
}

const hwmonRoot = "/sys/class/hwmon"

// Temperature publishes hwmon temperature sensors (CPU, NVMe, chipset, drives).
type Temperature struct{}

func (Temperature) Name() string { return "temperature" }

func (Temperature) Available() bool { return len(hwmonReadings(hwmonRoot)) > 0 }

func (Temperature) Collect(ctx context.Context) ([]model.Metric, error) {
	return tempMetrics(hwmonReadings(hwmonRoot)), nil
}
