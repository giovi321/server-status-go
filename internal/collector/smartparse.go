package collector

import (
	"encoding/json"
	"os"
	"strings"
)

// SmartInfo is the parsed subset of `smartctl --json -a` we publish.
// Optional attributes are pointers so "absent" is distinct from "zero".
type SmartInfo struct {
	Model, Serial, WWN, Firmware string
	CapacityBytes                int64
	RotationRate                 int
	Passed                       bool
	HasHealth                    bool

	Temperature          *int
	PowerOnHours         *int64
	PowerCycles          *int64
	Reallocated          *int64
	Pending              *int64
	OfflineUncorrectable *int64
	CRCErrors            *int64
	PercentageUsed       *int
	AvailableSpare       *int
	MediaErrors          *int64
	UnsafeShutdowns      *int64
	DataWrittenBytes     *int64
}

type smartRaw struct {
	ModelName    string `json:"model_name"`
	SerialNumber string `json:"serial_number"`
	Firmware     string `json:"firmware_version"`
	WWN          *struct {
		NAA int64 `json:"naa"`
		OUI int64 `json:"oui"`
		ID  int64 `json:"id"`
	} `json:"wwn"`
	UserCapacity struct {
		Bytes int64 `json:"bytes"`
	} `json:"user_capacity"`
	RotationRate int `json:"rotation_rate"`
	SmartStatus  *struct {
		Passed bool `json:"passed"`
	} `json:"smart_status"`
	Temperature *struct {
		Current int `json:"current"`
	} `json:"temperature"`
	ATA *struct {
		Table []struct {
			ID  int `json:"id"`
			Raw struct {
				Value int64 `json:"value"`
			} `json:"raw"`
		} `json:"table"`
	} `json:"ata_smart_attributes"`
	NVMe *struct {
		PercentageUsed   *int   `json:"percentage_used"`
		AvailableSpare   *int   `json:"available_spare"`
		MediaErrors      *int64 `json:"media_errors"`
		UnsafeShutdowns  *int64 `json:"unsafe_shutdowns"`
		PowerOnHours     *int64 `json:"power_on_hours"`
		PowerCycles      *int64 `json:"power_cycles"`
		DataUnitsWritten *int64 `json:"data_units_written"`
	} `json:"nvme_smart_health_information_log"`
}

func i64(v int64) *int64 { return &v }
func ip(v int) *int      { return &v }

// parseSmartctl parses `smartctl --json -a` output for ATA or NVMe devices.
func parseSmartctl(data []byte) (SmartInfo, error) {
	var r smartRaw
	if err := json.Unmarshal(data, &r); err != nil {
		return SmartInfo{}, err
	}
	si := SmartInfo{
		Model: r.ModelName, Serial: r.SerialNumber, Firmware: r.Firmware,
		CapacityBytes: r.UserCapacity.Bytes, RotationRate: r.RotationRate,
	}
	if r.SmartStatus != nil {
		si.HasHealth = true
		si.Passed = r.SmartStatus.Passed
	}
	if r.Temperature != nil {
		si.Temperature = ip(r.Temperature.Current)
	}
	if r.ATA != nil {
		for _, a := range r.ATA.Table {
			switch a.ID {
			case 5:
				si.Reallocated = i64(a.Raw.Value)
			case 9:
				si.PowerOnHours = i64(a.Raw.Value)
			case 12:
				si.PowerCycles = i64(a.Raw.Value)
			case 197:
				si.Pending = i64(a.Raw.Value)
			case 198:
				si.OfflineUncorrectable = i64(a.Raw.Value)
			case 199:
				si.CRCErrors = i64(a.Raw.Value)
			}
		}
	}
	if r.NVMe != nil {
		si.PercentageUsed = r.NVMe.PercentageUsed
		si.AvailableSpare = r.NVMe.AvailableSpare
		si.MediaErrors = r.NVMe.MediaErrors
		si.UnsafeShutdowns = r.NVMe.UnsafeShutdowns
		if r.NVMe.PowerOnHours != nil {
			si.PowerOnHours = r.NVMe.PowerOnHours
		}
		if r.NVMe.PowerCycles != nil {
			si.PowerCycles = r.NVMe.PowerCycles
		}
		if r.NVMe.DataUnitsWritten != nil {
			// NVMe data units are 512000-byte units (512 * 1000).
			si.DataWrittenBytes = i64(*r.NVMe.DataUnitsWritten * 512 * 1000)
		}
	}
	if r.WWN != nil {
		si.WWN = formatWWN(r.WWN.NAA, r.WWN.OUI, r.WWN.ID)
	}
	return si, nil
}

func formatWWN(naa, oui, id int64) string {
	// smartctl reports NAA/OUI/ID as separate integers; join as a stable hex string.
	b := strings.Builder{}
	b.WriteString(hex(naa))
	b.WriteString(hex6(oui))
	b.WriteString(hex9(id))
	return b.String()
}

func hex(v int64) string  { return trimHex(v, 1) }
func hex6(v int64) string { return trimHex(v, 6) }
func hex9(v int64) string { return trimHex(v, 9) }

func trimHex(v int64, width int) string {
	// simple fixed-width lowercase hex
	const digits = "0123456789abcdef"
	if v == 0 {
		return strings.Repeat("0", width)
	}
	var out []byte
	for v > 0 {
		out = append([]byte{digits[v&0xf]}, out...)
		v >>= 4
	}
	for len(out) < width {
		out = append([]byte{'0'}, out...)
	}
	return string(out)
}

// physicalDisks lists whole physical disks under a /sys/block-style root,
// excluding loop/ram/dm/md/sr/zram devices and partitions.
func physicalDisks(sysBlockRoot string) []string {
	entries, err := os.ReadDir(sysBlockRoot)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, "loop") || strings.HasPrefix(n, "ram") ||
			strings.HasPrefix(n, "dm-") || strings.HasPrefix(n, "md") ||
			strings.HasPrefix(n, "sr") || strings.HasPrefix(n, "zram") {
			continue
		}
		out = append(out, n)
	}
	return out
}
