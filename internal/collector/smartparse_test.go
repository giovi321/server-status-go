package collector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
)

// Trimmed but realistic `smartctl --json -a` output for an ATA HDD.
const smartctlATA = `{
  "model_name": "WDC WD40EFRX-68N32N0",
  "serial_number": "WD-WCC7K1234567",
  "firmware_version": "82.00A82",
  "user_capacity": {"bytes": 4000787030016},
  "rotation_rate": 5400,
  "smart_status": {"passed": true},
  "temperature": {"current": 34},
  "ata_smart_attributes": {"table": [
    {"id": 5,   "name": "Reallocated_Sector_Ct",   "raw": {"value": 0}},
    {"id": 9,   "name": "Power_On_Hours",           "raw": {"value": 21987}},
    {"id": 12,  "name": "Power_Cycle_Count",        "raw": {"value": 142}},
    {"id": 197, "name": "Current_Pending_Sector",   "raw": {"value": 0}},
    {"id": 198, "name": "Offline_Uncorrectable",    "raw": {"value": 0}},
    {"id": 199, "name": "UDMA_CRC_Error_Count",     "raw": {"value": 3}}
  ]}
}`

// Trimmed `smartctl --json -a` output for an NVMe SSD.
const smartctlNVMe = `{
  "model_name": "Samsung SSD 980 PRO 1TB",
  "serial_number": "S5GXNX0R123456",
  "firmware_version": "5B2QGXA7",
  "user_capacity": {"bytes": 1000204886016},
  "rotation_rate": 0,
  "smart_status": {"passed": true},
  "temperature": {"current": 41},
  "nvme_smart_health_information_log": {
    "percentage_used": 4,
    "available_spare": 100,
    "media_errors": 0,
    "unsafe_shutdowns": 12,
    "power_on_hours": 5123,
    "power_cycles": 88,
    "data_units_written": 41231234
  }
}`

func TestParseSmartctlATA(t *testing.T) {
	si, err := parseSmartctl([]byte(smartctlATA))
	if err != nil {
		t.Fatal(err)
	}
	if si.Model != "WDC WD40EFRX-68N32N0" || si.Serial != "WD-WCC7K1234567" {
		t.Fatalf("id: %+v", si)
	}
	if !si.Passed || si.CapacityBytes != 4000787030016 || si.RotationRate != 5400 {
		t.Fatalf("common: %+v", si)
	}
	if si.Temperature == nil || *si.Temperature != 34 {
		t.Fatalf("temp: %v", si.Temperature)
	}
	if si.PowerOnHours == nil || *si.PowerOnHours != 21987 {
		t.Fatalf("poh: %v", si.PowerOnHours)
	}
	if si.CRCErrors == nil || *si.CRCErrors != 3 {
		t.Fatalf("crc: %v", si.CRCErrors)
	}
	// NVMe-only attributes must be absent for an ATA disk
	if si.PercentageUsed != nil || si.AvailableSpare != nil {
		t.Fatalf("nvme attrs should be nil for ATA: %+v", si)
	}
}

func TestParseSmartctlNVMe(t *testing.T) {
	si, err := parseSmartctl([]byte(smartctlNVMe))
	if err != nil {
		t.Fatal(err)
	}
	if si.RotationRate != 0 {
		t.Fatalf("nvme rotation should be 0: %d", si.RotationRate)
	}
	if si.PercentageUsed == nil || *si.PercentageUsed != 4 {
		t.Fatalf("pct used: %v", si.PercentageUsed)
	}
	if si.AvailableSpare == nil || *si.AvailableSpare != 100 {
		t.Fatalf("avail spare: %v", si.AvailableSpare)
	}
	if si.DataWrittenBytes == nil || *si.DataWrittenBytes != 41231234*512*1000 {
		t.Fatalf("data written: %v", si.DataWrittenBytes)
	}
	// ATA-only attributes absent for NVMe
	if si.CRCErrors != nil || si.Reallocated != nil {
		t.Fatalf("ata attrs should be nil for NVMe: %+v", si)
	}
}

func TestSmartMetricsCuratedNVMe(t *testing.T) {
	si, _ := parseSmartctl([]byte(smartctlNVMe))
	cfg := config.Config{SmartAttributes: "curated", Disks: map[string]string{"S5GXNX0R123456": "OS drive"}}
	ms := smartMetrics(cfg, "nvme0n1", si)
	keys := map[string]model.Metric{}
	for _, m := range ms {
		keys[m.Key] = m
	}
	if _, ok := keys["disk_percentage_used"]; !ok {
		t.Fatal("nvme should emit disk_percentage_used")
	}
	if _, ok := keys["disk_crc_errors"]; ok {
		t.Fatal("nvme should NOT emit ata-only disk_crc_errors")
	}
	// alias applied and component derived from serial
	if keys["disk_temperature"].ComponentName != "OS drive" {
		t.Fatalf("alias: %q", keys["disk_temperature"].ComponentName)
	}
	if keys["disk_temperature"].Component != "disk-s5gxnx0r123456" {
		t.Fatalf("component: %q", keys["disk_temperature"].Component)
	}
	// no raw dump in curated mode
	if _, ok := keys["disk_smart_raw"]; ok {
		t.Fatal("curated mode must not emit disk_smart_raw")
	}
}

func TestPhysicalDisksFilters(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"sda", "nvme0n1", "vdb", "loop0", "ram0", "dm-0", "md0", "sr0", "zram0"} {
		if err := os.Mkdir(filepath.Join(root, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]bool{}
	for _, d := range physicalDisks(root) {
		got[d] = true
	}
	for _, want := range []string{"sda", "nvme0n1", "vdb"} {
		if !got[want] {
			t.Errorf("expected physical disk %q", want)
		}
	}
	for _, no := range []string{"loop0", "ram0", "dm-0", "md0", "sr0", "zram0"} {
		if got[no] {
			t.Errorf("%q should be excluded", no)
		}
	}
}
