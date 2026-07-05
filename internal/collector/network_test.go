package collector

import (
	"math"
	"testing"
)

const netdevFixture = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1000       10    0    0    0     0          0         0     1000      10    0    0    0     0       0          0
  eth0: 2000       20    0    0    0     0          0         0     3000      30    0    0    0     0       0          0
docker0:  500        5    0    0    0     0          0         0      600       6    0    0    0     0       0          0
`

func TestParseNetDevSkipsVirtual(t *testing.T) {
	m := parseNetDev(netdevFixture)
	if _, ok := m["lo"]; ok {
		t.Error("lo should be skipped")
	}
	if _, ok := m["docker0"]; ok {
		t.Error("docker0 should be skipped")
	}
	e, ok := m["eth0"]
	if !ok || e.RxBytes != 2000 || e.TxBytes != 3000 {
		t.Fatalf("eth0: %+v ok=%v", e, ok)
	}
}

func TestRate(t *testing.T) {
	// 1,048,576 bytes over 1s = 1 MB/s
	if got := rate(0, 1048576, 1.0); math.Abs(got-1.0) > 1e-6 {
		t.Fatalf("got %v", got)
	}
	// counter reset (cur < prev) => 0
	if got := rate(100, 10, 1.0); got != 0 {
		t.Fatalf("reset should be 0, got %v", got)
	}
}
