package collector

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/giovi321/server-status/internal/model"
)

// IfaceCounters holds cumulative byte counters for one interface.
type IfaceCounters struct {
	RxBytes uint64
	TxBytes uint64
}

func skipIface(name string) bool {
	if name == "lo" {
		return true
	}
	for _, p := range []string{"docker", "veth", "br-", "virbr", "tun", "tap", "kube", "cni", "flannel"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// parseNetDev parses /proc/net/dev, returning real interfaces only.
func parseNetDev(data string) map[string]IfaceCounters {
	out := map[string]IfaceCounters{}
	for _, line := range strings.Split(data, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || skipIface(name) {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 16 {
			continue
		}
		rx, err1 := strconv.ParseUint(f[0], 10, 64)
		tx, err2 := strconv.ParseUint(f[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out[name] = IfaceCounters{RxBytes: rx, TxBytes: tx}
	}
	return out
}

func rate(prev, cur uint64, seconds float64) float64 {
	if cur < prev || seconds <= 0 {
		return 0
	}
	return float64(cur-prev) / seconds / (1024.0 * 1024.0)
}

func operstate(iface string) bool {
	b, err := os.ReadFile("/sys/class/net/" + iface + "/operstate")
	if err != nil {
		return true // assume up if unknown
	}
	return strings.TrimSpace(string(b)) == "up"
}

func readNetDev() map[string]IfaceCounters {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil
	}
	return parseNetDev(string(data))
}

// Network publishes per-interface throughput. It is stateful: rates need two samples.
type Network struct {
	prev   map[string]IfaceCounters
	prevAt time.Time
}

func (Network) Name() string { return "network" }

func (Network) Available() bool { return len(readNetDev()) > 0 }

func (n *Network) Collect(ctx context.Context) ([]model.Metric, error) {
	cur := readNetDev()
	now := time.Now()
	var out []model.Metric
	if n.prev != nil {
		secs := now.Sub(n.prevAt).Seconds()
		for iface, c := range cur {
			p, ok := n.prev[iface]
			if !ok {
				continue
			}
			name := func(leaf string) string { return iface + " " + leaf }
			out = append(out,
				model.Metric{Key: "net_rx_rate", Instance: iface, Name: name("rx rate"), Value: round2(rate(p.RxBytes, c.RxBytes, secs)), Unit: "MB/s", StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:download-network"},
				model.Metric{Key: "net_tx_rate", Instance: iface, Name: name("tx rate"), Value: round2(rate(p.TxBytes, c.TxBytes, secs)), Unit: "MB/s", StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:upload-network"},
				model.Metric{Key: "net_operstate", Instance: iface, Name: name("link"), Value: operstate(iface), DeviceClass: "connectivity", Kind: model.KindBinarySensor, Category: "diagnostic"},
			)
		}
	}
	n.prev = cur
	n.prevAt = now
	return out, nil
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
