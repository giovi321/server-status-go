package collector

import (
	"context"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/giovi321/server-status/internal/model"
)

// Mount is one real mounted filesystem.
type Mount struct {
	Source   string
	Target   string
	FSType   string
	ReadOnly bool
}

// pseudoFS are filesystem types that never represent real block-backed storage.
var pseudoFS = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "tmpfs": true, "devpts": true,
	"cgroup": true, "cgroup2": true, "pstore": true, "bpf": true, "tracefs": true,
	"debugfs": true, "mqueue": true, "hugetlbfs": true, "securityfs": true,
	"fusectl": true, "configfs": true, "squashfs": true, "overlay": true,
	"autofs": true, "binfmt_misc": true, "rpc_pipefs": true, "nsfs": true, "ramfs": true,
}

// unescapeMountField decodes mountinfo octal escapes (\040 space, \011 tab, \012 newline, \134 backslash).
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			if n, err := strconv.ParseInt(s[i+1:i+4], 8, 16); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// parseMountinfo parses /proc/self/mountinfo and returns only real (block-backed) mounts.
// When a target is mounted more than once (bind/stacked mounts), the last (visible) one wins.
func parseMountinfo(data string) []Mount {
	var out []Mount
	idx := map[string]int{}
	for _, line := range strings.Split(data, "\n") {
		sep := strings.Index(line, " - ")
		if sep < 0 {
			continue
		}
		left := strings.Fields(line[:sep])
		right := strings.Fields(line[sep+3:])
		if len(left) < 6 || len(right) < 2 {
			continue
		}
		target := unescapeMountField(left[4])
		opts := left[5]
		fstype := right[0]
		source := unescapeMountField(right[1])
		if pseudoFS[fstype] {
			continue
		}
		ro := false
		for _, o := range strings.Split(opts, ",") {
			if o == "ro" {
				ro = true
			}
		}
		m := Mount{Source: source, Target: target, FSType: fstype, ReadOnly: ro}
		if p, ok := idx[target]; ok {
			out[p] = m
		} else {
			idx[target] = len(out)
			out = append(out, m)
		}
	}
	return out
}

// fsUsagePercent computes df-style used% from byte totals, guarding against
// garbage statfs data (e.g. 9p/NFS where free may exceed total).
func fsUsagePercent(total, free, avail uint64) int {
	if free > total {
		return 0
	}
	used := total - free
	denom := used + avail
	if denom == 0 {
		return 0
	}
	p := int(float64(used)*100.0/float64(denom) + 0.5)
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

// fsInodePercent computes inode usage%, guarding against garbage statfs data
// (files==0, or ffree > files which would underflow a uint64 subtraction).
func fsInodePercent(files, ffree uint64) int {
	if files == 0 || ffree > files {
		return 0
	}
	p := int(float64(files-ffree)*100.0/float64(files) + 0.5)
	if p > 100 {
		return 100
	}
	return p
}

func mountMetrics(m Mount) []model.Metric {
	inst := m.Target
	label := m.Target
	if label == "/" {
		label = "root"
	}
	var st unix.Statfs_t
	if err := unix.Statfs(m.Target, &st); err != nil || st.Blocks == 0 {
		return nil
	}
	bs := uint64(st.Bsize)
	total := st.Blocks * bs
	free := st.Bfree * bs
	avail := st.Bavail * bs
	var used uint64
	if total > free {
		used = total - free
	}
	usagePct := fsUsagePercent(total, free, avail)
	inodePct := fsInodePercent(st.Files, st.Ffree)
	name := func(leaf string) string { return label + " " + leaf }
	return []model.Metric{
		{Key: "fs_usage", Instance: inst, Name: name("usage"), Value: usagePct, Unit: "%", StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:harddisk"},
		{Key: "fs_used_bytes", Instance: inst, Name: name("used"), Value: int64(used), Unit: "B", DeviceClass: "data_size", StateClass: "measurement", Kind: model.KindSensor, Category: "diagnostic"},
		{Key: "fs_total_bytes", Instance: inst, Name: name("size"), Value: int64(total), Unit: "B", DeviceClass: "data_size", StateClass: "measurement", Kind: model.KindSensor, Category: "diagnostic"},
		{Key: "fs_inode_usage", Instance: inst, Name: name("inode usage"), Value: inodePct, Unit: "%", StateClass: "measurement", Kind: model.KindSensor, Category: "diagnostic"},
		{Key: "fs_type", Instance: inst, Name: name("filesystem"), Value: m.FSType, Kind: model.KindText, Category: "diagnostic"},
		{Key: "fs_read_only", Instance: inst, Name: name("read only"), Value: m.ReadOnly, DeviceClass: "problem", Kind: model.KindBinarySensor, Category: "diagnostic"},
	}
}

func readMounts() []Mount {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	return parseMountinfo(string(data))
}

// Filesystem publishes usage and health for each real mounted filesystem.
type Filesystem struct{}

func (Filesystem) Name() string { return "filesystem" }

func (Filesystem) Available() bool { return len(readMounts()) > 0 }

func (Filesystem) Collect(ctx context.Context) ([]model.Metric, error) {
	var out []model.Metric
	for _, m := range readMounts() {
		out = append(out, mountMetrics(m)...)
	}
	return out, nil
}
