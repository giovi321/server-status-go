package collector

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHwmonReadings(t *testing.T) {
	root := t.TempDir()
	// hwmon0: coretemp with a labeled Package sensor
	writeFile(t, filepath.Join(root, "hwmon0", "name"), "coretemp\n")
	writeFile(t, filepath.Join(root, "hwmon0", "temp1_input"), "45000\n")
	writeFile(t, filepath.Join(root, "hwmon0", "temp1_label"), "Package id 0\n")
	// hwmon1: nvme, no label -> falls back to chip name + tempN
	writeFile(t, filepath.Join(root, "hwmon1", "name"), "nvme\n")
	writeFile(t, filepath.Join(root, "hwmon1", "temp1_input"), "38000\n")

	got := map[string]int{}
	for _, r := range hwmonReadings(root) {
		got[r.Label] = r.MilliC
	}
	if got["Package id 0"] != 45000 {
		t.Fatalf("labeled reading: %+v", got)
	}
	if got["nvme temp1"] != 38000 {
		t.Fatalf("unlabeled reading fallback: %+v", got)
	}
}

func TestTempMetricsDistinctInstancePerChip(t *testing.T) {
	// two chips with the SAME label (e.g. two NVMe "Composite", or two drivetemp) must not collide
	rs := []TempReading{
		{Chip: "hwmon5", Sensor: "temp1", Label: "drivetemp temp1", MilliC: 40000},
		{Chip: "hwmon6", Sensor: "temp1", Label: "drivetemp temp1", MilliC: 42000},
	}
	ms := tempMetrics(rs)
	if len(ms) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(ms))
	}
	if ms[0].Instance == ms[1].Instance {
		t.Fatalf("identically-labeled sensors on different chips must have distinct Instance, both = %q", ms[0].Instance)
	}
}
