package collector

import (
	"math"
	"testing"
)

func TestParseUptime(t *testing.T) {
	days, ok := parseUptime("172800.00 100000.00")
	if !ok {
		t.Fatal("expected ok")
	}
	if math.Abs(days-2.0) > 0.001 { // 172800s = 2 days
		t.Fatalf("got %v", days)
	}
	if _, ok := parseUptime("garbage"); ok {
		t.Fatal("garbage should not parse")
	}
}

func TestUptimeMetricPrecision(t *testing.T) {
	// Under 10 days keeps two decimals; 10+ days rounds to an integer.
	if v := uptimeMetric(2.5).Value; v != 2.5 {
		t.Fatalf("under 10 days: %v", v)
	}
	if v := uptimeMetric(42.7).Value; v != 43 {
		t.Fatalf("over 10 days: %v", v)
	}
}
