package collector

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// ZpoolInfo is one ZFS pool from `zpool list -H -o name,health,cap,frag`.
type ZpoolInfo struct {
	Name, Health string
	CapacityPct  int
	FragPct      int
	HasFrag      bool
}

func pctField(s string) (int, bool) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	if s == "" || s == "-" {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return v, true
}

// parseZpoolList parses `zpool list -H -o name,health,cap,frag` (tab-separated).
func parseZpoolList(out string) []ZpoolInfo {
	var pools []ZpoolInfo
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		z := ZpoolInfo{Name: f[0], Health: f[1]}
		if cap, ok := pctField(f[2]); ok {
			z.CapacityPct = cap
		}
		if frag, ok := pctField(f[3]); ok {
			z.FragPct = frag
			z.HasFrag = true
		}
		pools = append(pools, z)
	}
	return pools
}

func zfsMetrics(z ZpoolInfo) []model.Metric {
	comp := "pool-" + z.Name
	name := "Pool " + z.Name
	out := []model.Metric{
		{Key: "pool_health", Component: comp, ComponentName: name, Name: "Health", Value: z.Health, Kind: model.KindText, Category: "primary"},
		{Key: "pool_degraded", Component: comp, ComponentName: name, Name: "Degraded", Value: z.Health != "ONLINE", DeviceClass: "problem", Kind: model.KindBinarySensor, Category: "primary"},
		{Key: "pool_capacity", Component: comp, ComponentName: name, Name: "Capacity", Value: z.CapacityPct, Unit: "%", StateClass: "measurement", Kind: model.KindSensor, Category: "primary"},
	}
	if z.HasFrag {
		out = append(out, model.Metric{Key: "pool_fragmentation", Component: comp, ComponentName: name, Name: "Fragmentation", Value: z.FragPct, Unit: "%", StateClass: "measurement", Kind: model.KindSensor, Category: "diagnostic"})
	}
	return out
}

func zpoolPath() string {
	for _, p := range []string{"/usr/sbin/zpool", "/sbin/zpool", "/usr/bin/zpool"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func readZpools(ctx context.Context) []ZpoolInfo {
	bin := zpoolPath()
	if bin == "" {
		return nil
	}
	out, err := exec.CommandContext(ctx, bin, "list", "-H", "-o", "name,health,cap,frag").Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	return parseZpoolList(string(out))
}

// Zfs publishes per-pool ZFS health as sub-devices.
type Zfs struct{}

func (Zfs) Name() string { return "zfs" }

func (Zfs) Available() bool { return zpoolPath() != "" }

func (Zfs) Collect(ctx context.Context) ([]model.Metric, error) {
	var out []model.Metric
	for _, z := range readZpools(ctx) {
		out = append(out, zfsMetrics(z)...)
	}
	return out, nil
}
