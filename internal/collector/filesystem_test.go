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
