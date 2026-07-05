package collector

import (
	"math"
	"testing"
)

func TestParseLoadAvg(t *testing.T) {
	v, ok := parseLoadAvg("0.52 0.58 0.59 1/834 12345")
	if !ok {
		t.Fatal("expected ok")
	}
	if math.Abs(v[0]-0.52) > 1e-9 || math.Abs(v[1]-0.58) > 1e-9 || math.Abs(v[2]-0.59) > 1e-9 {
		t.Fatalf("got %v", v)
	}
	if _, ok := parseLoadAvg("garbage"); ok {
		t.Fatal("garbage should not parse")
	}
}

func TestLoadMetrics(t *testing.T) {
	m := loadMetrics([3]float64{0.5, 0.6, 0.7})
	got := map[string]any{}
	for _, mt := range m {
		got[mt.Key] = mt.Value
	}
	if got["load_1m"] != 0.5 || got["load_5m"] != 0.6 || got["load_15m"] != 0.7 {
		t.Fatalf("got %+v", got)
	}
}
