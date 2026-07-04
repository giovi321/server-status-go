package collector

import (
	"math"
	"testing"
)

func TestParseCPUSample(t *testing.T) {
	// user nice system idle iowait irq softirq steal
	s, ok := parseCPUSample("cpu  100 0 50 800 20 0 30 0")
	if !ok {
		t.Fatal("expected parse ok")
	}
	// idle_all = idle+iowait = 820; non_idle = user+nice+system+irq+softirq(+steal) = 100+0+50+0+30+0 = 180
	if s.Idle != 820 || s.Total != 1000 {
		t.Fatalf("got idle=%d total=%d", s.Idle, s.Total)
	}
}

func TestParseCPUSampleRejectsOtherLines(t *testing.T) {
	if _, ok := parseCPUSample("cpu0 1 2 3 4 5 6 7"); ok {
		t.Fatal("only the aggregate cpu line should parse")
	}
	if _, ok := parseCPUSample("intr 1234"); ok {
		t.Fatal("non-cpu line should not parse")
	}
}

func TestUsagePercent(t *testing.T) {
	a := CPUSample{Idle: 100, Total: 200}
	b := CPUSample{Idle: 150, Total: 400} // delta idle 50, delta total 200 => 75% busy
	got := usagePercent(a, b)
	if math.Abs(got-75.0) > 0.001 {
		t.Fatalf("got %v", got)
	}
}
