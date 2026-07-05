package collector

import (
	"testing"

	"github.com/giovi321/server-status/internal/model"
)

// nvidia-smi --query-gpu=index,name,temperature.gpu,utilization.gpu,memory.total,memory.used,power.draw,power.limit,fan.speed,driver_version --format=csv,noheader,nounits
const nvidiaSMICSV = `0, NVIDIA GeForce RTX 3060, 45, 12, 12288, 2048, 80.50, 170.00, 30, 535.104.05
1, NVIDIA Tesla T4, 38, 0, 15360, 512, 26.30, 70.00, [N/A], 535.104.05`

func TestParseNvidiaSMI(t *testing.T) {
	gpus := parseNvidiaSMI(nvidiaSMICSV)
	if len(gpus) != 2 {
		t.Fatalf("expected 2 gpus, got %d", len(gpus))
	}
	g0 := gpus[0]
	if g0.Index != "0" || g0.Name != "NVIDIA GeForce RTX 3060" {
		t.Fatalf("g0 id: %+v", g0)
	}
	if g0.Temp != 45 || g0.Util != 12 || g0.MemTotalMB != 12288 || g0.MemUsedMB != 2048 {
		t.Fatalf("g0 core: %+v", g0)
	}
	if g0.Power == nil || *g0.Power != 80.50 || g0.Fan == nil || *g0.Fan != 30 {
		t.Fatalf("g0 power/fan: %v %v", g0.Power, g0.Fan)
	}
	if g0.Driver != "535.104.05" {
		t.Fatalf("g0 driver: %q", g0.Driver)
	}
	// Tesla T4 has no fan (datacenter card): [N/A] -> nil
	if gpus[1].Fan != nil {
		t.Fatalf("g1 fan should be nil, got %v", *gpus[1].Fan)
	}
}

func TestGpuMetricsMemoryPercent(t *testing.T) {
	gpus := parseNvidiaSMI(nvidiaSMICSV)
	ms := gpuMetrics(gpus[0])
	by := map[string]model.Metric{}
	for _, m := range ms {
		by[m.Key] = m
	}
	// 2048/12288 = 16.67% -> 17
	if by["gpu_memory_used"].Value != 17 {
		t.Fatalf("mem used pct: %v", by["gpu_memory_used"].Value)
	}
	if by["gpu_temperature"].Component != "gpu-0" || by["gpu_temperature"].ComponentName != "GPU 0" {
		t.Fatalf("sub-device: %+v", by["gpu_temperature"])
	}
	// no fan metric for a card whose fan is N/A would be absent; here g0 has a fan
	if _, ok := by["gpu_fan"]; !ok {
		t.Fatal("g0 should have gpu_fan")
	}
}
