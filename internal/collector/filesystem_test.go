package collector

import "testing"

// A trimmed /proc/self/mountinfo sample: real root + a data mount, plus pseudo fs to filter.
const mountinfoFixture = `22 28 0:21 / /proc rw,nosuid shared:14 - proc proc rw
23 28 0:22 / /sys rw,nosuid shared:15 - sysfs sysfs rw
24 28 0:5 / /dev rw,nosuid shared:2 - devtmpfs devtmpfs rw,size=1234k
28 1 254:0 / / rw,relatime shared:1 - ext4 /dev/mapper/vg-root rw
40 28 254:1 / /mnt/storage ro,relatime shared:29 - xfs /dev/mapper/vg-storage ro
55 28 0:44 / /run/snapd/ns rw shared:99 - tmpfs tmpfs rw
77 28 7:0 / /snap/core/1 ro,nodev - squashfs /dev/loop0 ro
`

func TestParseMountinfoFiltersPseudo(t *testing.T) {
	mounts := parseMountinfo(mountinfoFixture)
	byTarget := map[string]Mount{}
	for _, m := range mounts {
		byTarget[m.Target] = m
	}
	if _, ok := byTarget["/proc"]; ok {
		t.Error("/proc should be filtered")
	}
	if _, ok := byTarget["/run/snapd/ns"]; ok {
		t.Error("tmpfs should be filtered")
	}
	if _, ok := byTarget["/snap/core/1"]; ok {
		t.Error("squashfs should be filtered")
	}
	root, ok := byTarget["/"]
	if !ok || root.FSType != "ext4" || root.ReadOnly {
		t.Fatalf("root: %+v ok=%v", root, ok)
	}
	st, ok := byTarget["/mnt/storage"]
	if !ok || st.FSType != "xfs" || !st.ReadOnly {
		t.Fatalf("storage: %+v ok=%v", st, ok)
	}
}

func TestParseMountinfoDedupKeepsLast(t *testing.T) {
	in := "100 1 8:1 / /data rw,relatime shared:1 - ext4 /dev/sdb1 rw\n" +
		"101 1 8:2 / /data ro,relatime shared:2 - xfs /dev/sdc1 ro\n"
	mounts := parseMountinfo(in)
	n := 0
	var got Mount
	for _, m := range mounts {
		if m.Target == "/data" {
			n++
			got = m
		}
	}
	if n != 1 {
		t.Fatalf("expected 1 /data entry, got %d", n)
	}
	if got.FSType != "xfs" || !got.ReadOnly {
		t.Fatalf("expected last-seen xfs ro, got %+v", got)
	}
}

func TestParseMountinfoUnescapesTarget(t *testing.T) {
	in := "100 1 8:1 / /mnt/my\\040drive rw - ext4 /dev/sdd1 rw\n"
	mounts := parseMountinfo(in)
	if len(mounts) != 1 || mounts[0].Target != "/mnt/my drive" {
		t.Fatalf("expected decoded '/mnt/my drive', got %+v", mounts)
	}
}

func TestFsUsagePercent(t *testing.T) {
	if got := fsUsagePercent(1000, 250, 250); got != 75 { // used=750, denom=used+avail=1000
		t.Fatalf("normal: %d", got)
	}
	if got := fsUsagePercent(1000, 2000, 0); got != 0 {
		t.Fatalf("free>total guard: %d", got)
	}
	if got := fsUsagePercent(0, 0, 0); got != 0 {
		t.Fatalf("zero: %d", got)
	}
}

func TestFsInodePercent(t *testing.T) {
	if got := fsInodePercent(1000, 250); got != 75 {
		t.Fatalf("normal: %d", got)
	}
	if got := fsInodePercent(1000, 2000); got != 0 {
		t.Fatalf("ffree>files underflow guard: %d", got)
	}
	if got := fsInodePercent(0, 0); got != 0 {
		t.Fatalf("zero files: %d", got)
	}
}
