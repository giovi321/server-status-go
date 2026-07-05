package collector

import "testing"

// /proc/mdstat: a healthy md0 (raid1) and a degraded+resyncing md1 (raid5).
const mdstatFixture = `Personalities : [raid1] [raid6] [raid5] [raid4]
md0 : active raid1 sdb1[1] sda1[0]
      3906887168 blocks super 1.2 [2/2] [UU]

md1 : active raid5 sdd1[3] sdc1[1] sde1[2]
      7813772800 blocks super 1.2 level 5, 512k chunk, algorithm 2 [4/3] [UU_U]
      [==========>..........]  recovery = 52.3% (1234567/2345678) finish=100.0min speed=100000K/sec

unused devices: <none>
`

func TestParseMdstat(t *testing.T) {
	arrays := parseMdstat(mdstatFixture)
	by := map[string]RaidArray{}
	for _, a := range arrays {
		by[a.Name] = a
	}
	md0, ok := by["md0"]
	if !ok || md0.Level != "raid1" || md0.Total != 2 || md0.Active != 2 || md0.Failed != 0 {
		t.Fatalf("md0: %+v ok=%v", md0, ok)
	}
	md1, ok := by["md1"]
	if !ok || md1.Total != 4 || md1.Active != 3 || md1.Failed != 1 {
		t.Fatalf("md1: %+v", md1)
	}
	if !md1.ResyncActive || md1.ResyncPct != 52 {
		t.Fatalf("md1 resync: active=%v pct=%d", md1.ResyncActive, md1.ResyncPct)
	}
}
