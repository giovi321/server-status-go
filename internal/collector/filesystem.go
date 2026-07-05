package collector

import (
	"context"
	"os"
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

// parseMountinfo parses /proc/self/mountinfo and returns only real (block-backed) mounts.
// Format: "id parent maj:min root mountpoint opts... - fstype source superopts".
func parseMountinfo(data string) []Mount {
	var out []Mount
	seen := map[string]bool{}
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
		target := left[4]
		opts := left[5]
		fstype := right[0]
		source := right[1]
		if pseudoFS[fstype] {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		ro := false
		for _, o := range strings.Split(opts, ",") {
			if o == "ro" {
				ro = true
			}
		}
		out = append(out, Mount{Source: source, Target: target, FSType: fstype, ReadOnly: ro})
	}
	return out
}

func mountMetrics(m Mount) []model.Metric {
	inst := m.Target
	var st unix.Statfs_t
	if err := unix.Statfs(m.Target, &st); err != nil || st.Blocks == 0 {
		return nil
	}
	total := st.Blocks * uint64(st.Bsize)
	free := st.Bfree * uint64(st.Bsize)
	used := total - free
	usagePct := int(float64(used)*100.0/float64(total) + 0.5)
	var inodePct int
	if st.Files > 0 {
		inodePct = int(float64(st.Files-st.Ffree)*100.0/float64(st.Files) + 0.5)
	}
	name := func(leaf string) string { return m.Target + " " + leaf }
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
