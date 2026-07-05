package collector

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// GpuInfo is one GPU parsed from nvidia-smi CSV. Optional fields are pointers.
type GpuInfo struct {
	Index, Name           string
	Temp, Util            int
	MemTotalMB, MemUsedMB int
	Power, PowerLimit     *float64
	Fan                   *int
	Driver                string
}

func naField(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || s == "[N/A]" || s == "[Not Supported]" || s == "[Unknown Error]"
}

// parseNvidiaSMI parses `nvidia-smi --query-gpu=index,name,temperature.gpu,
// utilization.gpu,memory.total,memory.used,power.draw,power.limit,fan.speed,
// driver_version --format=csv,noheader,nounits` output (one GPU per line).
func parseNvidiaSMI(csv string) []GpuInfo {
	var out []GpuInfo
	for _, line := range strings.Split(csv, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, ",")
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
		}
		if len(f) < 10 {
			continue
		}
		g := GpuInfo{Index: f[0], Name: f[1], Driver: f[9]}
		g.Temp, _ = strconv.Atoi(f[2])
		g.Util, _ = strconv.Atoi(f[3])
		g.MemTotalMB, _ = strconv.Atoi(f[4])
		g.MemUsedMB, _ = strconv.Atoi(f[5])
		if !naField(f[6]) {
			if v, err := strconv.ParseFloat(f[6], 64); err == nil {
				g.Power = &v
			}
		}
		if !naField(f[7]) {
			if v, err := strconv.ParseFloat(f[7], 64); err == nil {
				g.PowerLimit = &v
			}
		}
		if !naField(f[8]) {
			if v, err := strconv.Atoi(f[8]); err == nil {
				g.Fan = &v
			}
		}
		out = append(out, g)
	}
	return out
}

func gpuMetrics(g GpuInfo) []model.Metric {
	comp := "gpu-" + g.Index
	name := "GPU " + g.Index
	sensor := func(key, leaf string, val any, unit, dc string) model.Metric {
		return model.Metric{Key: key, Component: comp, ComponentName: name, Name: leaf, Value: val, Unit: unit, DeviceClass: dc, StateClass: "measurement", Kind: model.KindSensor, Category: "primary"}
	}
	out := []model.Metric{
		sensor("gpu_temperature", "Temperature", g.Temp, "°C", "temperature"),
		sensor("gpu_utilization", "Utilization", g.Util, "%", ""),
	}
	if g.MemTotalMB > 0 {
		pctUsed := int(float64(g.MemUsedMB)*100.0/float64(g.MemTotalMB) + 0.5)
		out = append(out, sensor("gpu_memory_used", "Memory used", pctUsed, "%", ""))
	}
	if g.Power != nil {
		out = append(out, sensor("gpu_power", "Power", *g.Power, "W", "power"))
	}
	if g.PowerLimit != nil {
		out = append(out, model.Metric{Key: "gpu_power_limit", Component: comp, ComponentName: name, Name: "Power limit", Value: *g.PowerLimit, Unit: "W", DeviceClass: "power", Kind: model.KindSensor, Category: "diagnostic"})
	}
	if g.Fan != nil {
		out = append(out, sensor("gpu_fan", "Fan", *g.Fan, "%", ""))
	}
	if g.Name != "" {
		out = append(out, model.Metric{Key: "gpu_name", Component: comp, ComponentName: name, Name: "Name", Value: g.Name, Kind: model.KindText, Category: "diagnostic"})
	}
	if g.Driver != "" {
		out = append(out, model.Metric{Key: "gpu_driver", Component: comp, ComponentName: name, Name: "Driver", Value: g.Driver, Kind: model.KindText, Category: "diagnostic"})
	}
	return out
}

func nvidiaSMIPath() string {
	for _, p := range []string{"/usr/bin/nvidia-smi", "/bin/nvidia-smi"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

const nvidiaQuery = "index,name,temperature.gpu,utilization.gpu,memory.total,memory.used,power.draw,power.limit,fan.speed,driver_version"

// Gpu publishes per-GPU metrics as sub-devices via nvidia-smi.
type Gpu struct{}

func (Gpu) Name() string { return "gpu" }

func (Gpu) Available() bool { return nvidiaSMIPath() != "" }

func (Gpu) Collect(ctx context.Context) ([]model.Metric, error) {
	bin := nvidiaSMIPath()
	if bin == "" {
		return nil, nil
	}
	out, err := exec.CommandContext(ctx, bin, "--query-gpu="+nvidiaQuery, "--format=csv,noheader,nounits").Output()
	if err != nil && len(out) == 0 {
		return nil, nil
	}
	var metrics []model.Metric
	for _, g := range parseNvidiaSMI(string(out)) {
		metrics = append(metrics, gpuMetrics(g)...)
	}
	return metrics, nil
}
