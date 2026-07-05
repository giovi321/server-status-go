package collector

import "testing"

func TestSwapMetric(t *testing.T) {
	mem := map[string]uint64{"SwapTotal": 1000, "SwapFree": 250}
	m, ok := swapMetric(mem)
	if !ok {
		t.Fatal("expected ok")
	}
	if m.Key != "swap_used" || m.Value != 75 { // used 750/1000 = 75%
		t.Fatalf("got %+v", m)
	}
}

func TestSwapMetricNoSwap(t *testing.T) {
	if _, ok := swapMetric(map[string]uint64{"SwapTotal": 0, "SwapFree": 0}); ok {
		t.Fatal("no swap configured should not emit")
	}
}
