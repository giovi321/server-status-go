package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/ha"
	"github.com/giovi321/server-status/internal/model"
)

// Smart publishes SMART health per physical disk as a sub-device. It caches
// results in memory on a min-interval so it does not wake drives every cycle.
type Smart struct {
	cfg      config.Config
	interval time.Duration
	mu       sync.Mutex
	cached   []model.Metric
	cachedAt time.Time
}

func NewSmart(cfg config.Config) *Smart {
	iv := 1800 * time.Second
	return &Smart{cfg: cfg, interval: iv}
}

func smartctlPath() string {
	for _, p := range []string{"/usr/sbin/smartctl", "/sbin/smartctl", "/usr/bin/smartctl"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func (*Smart) Name() string { return "smart" }

func (*Smart) Available() bool {
	return smartctlPath() != "" && len(physicalDisks("/sys/block")) > 0
}

func (s *Smart) Collect(ctx context.Context) ([]model.Metric, error) {
	s.mu.Lock()
	fresh := s.cached != nil && time.Since(s.cachedAt) < s.interval
	cached := s.cached
	s.mu.Unlock()
	if fresh {
		return cached, nil
	}
	bin := smartctlPath()
	if bin == "" {
		return nil, nil
	}
	var out []model.Metric
	for _, disk := range physicalDisks("/sys/block") {
		data, err := exec.CommandContext(ctx, bin, "--json", "-a", "/dev/"+disk).Output()
		if err != nil && len(data) == 0 {
			continue // smartctl exits non-zero with warnings but still prints JSON; skip only if truly empty
		}
		si, perr := parseSmartctl(data)
		if perr != nil {
			continue
		}
		out = append(out, smartMetrics(s.cfg, disk, si)...)
	}
	s.mu.Lock()
	s.cached = out
	s.cachedAt = time.Now()
	s.mu.Unlock()
	return out, nil
}

func smartComponent(disk string, si SmartInfo) string {
	id := si.Serial
	if id == "" {
		id = si.WWN
	}
	if id == "" {
		id = disk
	}
	return "disk-" + ha.InstanceSlug(id)
}

func smartMetrics(cfg config.Config, disk string, si SmartInfo) []model.Metric {
	comp := smartComponent(disk, si)
	name := cfg.DiskName(si.Serial, "Disk "+disk)
	sensor := func(key, leaf string, val any, unit, dc, sc string) model.Metric {
		return model.Metric{Key: key, Component: comp, ComponentName: name, Name: leaf, Value: val, Unit: unit, DeviceClass: dc, StateClass: sc, Kind: model.KindSensor, Category: "primary"}
	}
	diag := func(key, leaf string, val any) model.Metric {
		return model.Metric{Key: key, Component: comp, ComponentName: name, Name: leaf, Value: val, Kind: model.KindText, Category: "diagnostic"}
	}
	var out []model.Metric
	if si.HasHealth {
		out = append(out, model.Metric{Key: "disk_health", Component: comp, ComponentName: name, Name: "Health", Value: !si.Passed, DeviceClass: "problem", Kind: model.KindBinarySensor, Category: "primary"})
	}
	if si.Temperature != nil {
		out = append(out, sensor("disk_temperature", "Temperature", *si.Temperature, "°C", "temperature", "measurement"))
	}
	if si.PowerOnHours != nil {
		out = append(out, sensor("disk_power_on_hours", "Power on hours", *si.PowerOnHours, "h", "", "total_increasing"))
	}
	if si.PowerCycles != nil {
		out = append(out, sensor("disk_power_cycles", "Power cycles", *si.PowerCycles, "", "", "total_increasing"))
	}
	if si.Reallocated != nil {
		out = append(out, sensor("disk_reallocated_sectors", "Reallocated sectors", *si.Reallocated, "", "", "measurement"))
	}
	if si.Pending != nil {
		out = append(out, sensor("disk_pending_sectors", "Pending sectors", *si.Pending, "", "", "measurement"))
	}
	if si.OfflineUncorrectable != nil {
		out = append(out, sensor("disk_offline_uncorrectable", "Offline uncorrectable", *si.OfflineUncorrectable, "", "", "measurement"))
	}
	if si.CRCErrors != nil {
		out = append(out, sensor("disk_crc_errors", "CRC errors", *si.CRCErrors, "", "", "measurement"))
	}
	if si.PercentageUsed != nil {
		out = append(out, sensor("disk_percentage_used", "Percentage used", *si.PercentageUsed, "%", "", "measurement"))
	}
	if si.AvailableSpare != nil {
		out = append(out, sensor("disk_available_spare", "Available spare", *si.AvailableSpare, "%", "", "measurement"))
	}
	if si.MediaErrors != nil {
		out = append(out, sensor("disk_media_errors", "Media errors", *si.MediaErrors, "", "", "measurement"))
	}
	if si.UnsafeShutdowns != nil {
		out = append(out, sensor("disk_unsafe_shutdowns", "Unsafe shutdowns", *si.UnsafeShutdowns, "", "", "total_increasing"))
	}
	if si.DataWrittenBytes != nil {
		out = append(out, sensor("disk_data_written", "Data written", *si.DataWrittenBytes, "B", "data_size", "total_increasing"))
	}
	if si.Model != "" {
		out = append(out, diag("disk_model", "Model", si.Model))
	}
	if si.Serial != "" {
		out = append(out, diag("disk_serial", "Serial", si.Serial))
	}
	if si.Firmware != "" {
		out = append(out, diag("disk_firmware", "Firmware", si.Firmware))
	}
	if si.CapacityBytes > 0 {
		out = append(out, model.Metric{Key: "disk_capacity", Component: comp, ComponentName: name, Name: "Capacity", Value: si.CapacityBytes, Unit: "B", DeviceClass: "data_size", Kind: model.KindSensor, Category: "diagnostic"})
	}
	rot := "SSD"
	if si.RotationRate > 0 {
		rot = fmt.Sprintf("%d rpm", si.RotationRate)
	}
	out = append(out, diag("disk_rotation", "Rotation", rot))
	if strings.EqualFold(cfg.SmartAttributes, "full") {
		if raw, err := json.Marshal(si); err == nil {
			out = append(out, diag("disk_smart_raw", "SMART raw", string(raw)))
		}
	}
	return out
}
