package collector

import "testing"

const meminfoFixture = `MemTotal:       16384000 kB
MemFree:         1000000 kB
MemAvailable:    8192000 kB
Buffers:          500000 kB
Cached:          4000000 kB
`

func TestParseMeminfo(t *testing.T) {
	m, ok := parseMeminfo(meminfoFixture)
	if !ok {
		t.Fatal("expected ok")
	}
	if m["MemTotal"] != 16384000 || m["MemAvailable"] != 8192000 {
		t.Fatalf("got %+v", m)
	}
}

func TestMemoryMetrics(t *testing.T) {
	m, _ := parseMeminfo(meminfoFixture)
	metrics := memoryMetrics(m)
	got := map[string]any{}
	for _, mt := range metrics {
		got[mt.Key] = mt.Value
	}
	// available = 8192000/16384000 = 50%, used = 50%
	if got["memory_available"] != 50 {
		t.Fatalf("memory_available=%v", got["memory_available"])
	}
	if got["memory_used"] != 50 {
		t.Fatalf("memory_used=%v", got["memory_used"])
	}
}
