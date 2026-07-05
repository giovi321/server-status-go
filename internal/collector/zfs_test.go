package collector

import (
	"testing"

	"github.com/giovi321/server-status/internal/model"
)

// `zpool list -H -o name,health,cap,frag` (tab-separated, no header).
const zpoolListOut = "tank\tONLINE\t52%\t3%\nbackup\tDEGRADED\t78%\t-\n"

func TestParseZpoolList(t *testing.T) {
	pools := parseZpoolList(zpoolListOut)
	by := map[string]ZpoolInfo{}
	for _, p := range pools {
		by[p.Name] = p
	}
	tank, ok := by["tank"]
	if !ok || tank.Health != "ONLINE" || tank.CapacityPct != 52 || !tank.HasFrag || tank.FragPct != 3 {
		t.Fatalf("tank: %+v ok=%v", tank, ok)
	}
	backup, ok := by["backup"]
	if !ok || backup.Health != "DEGRADED" || backup.CapacityPct != 78 || backup.HasFrag {
		t.Fatalf("backup: %+v (frag '-' should mean HasFrag=false)", backup)
	}
}

func TestZfsMetricsDegraded(t *testing.T) {
	pools := parseZpoolList(zpoolListOut)
	var backup ZpoolInfo
	for _, p := range pools {
		if p.Name == "backup" {
			backup = p
		}
	}
	ms := zfsMetrics(backup)
	by := map[string]model.Metric{}
	for _, m := range ms {
		by[m.Key] = m
	}
	if by["pool_degraded"].Value != true {
		t.Fatal("DEGRADED pool must report pool_degraded=true")
	}
	if by["pool_health"].Component != "pool-backup" || by["pool_health"].ComponentName != "Pool backup" {
		t.Fatalf("sub-device: %+v", by["pool_health"])
	}
	// frag '-' -> no fragmentation metric
	if _, ok := by["pool_fragmentation"]; ok {
		t.Fatal("backup has no fragmentation ('-') so no metric")
	}
}
