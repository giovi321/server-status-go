package collector

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// parseFailedUnits extracts unit names from `systemctl --failed --plain --no-legend`.
func parseFailedUnits(out string) []string {
	var units []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if strings.HasSuffix(f[0], ".service") || strings.HasSuffix(f[0], ".socket") ||
			strings.HasSuffix(f[0], ".timer") || strings.HasSuffix(f[0], ".mount") || strings.HasSuffix(f[0], ".target") {
			units = append(units, f[0])
		}
	}
	return units
}

func systemctlPath() string {
	for _, p := range []string{"/usr/bin/systemctl", "/bin/systemctl"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Systemd publishes the count and list of failed systemd units.
type Systemd struct{}

func (Systemd) Name() string { return "systemd" }

func (Systemd) Available() bool { return systemctlPath() != "" }

func (Systemd) Collect(ctx context.Context) ([]model.Metric, error) {
	p := systemctlPath()
	if p == "" {
		return nil, nil
	}
	out, _ := exec.CommandContext(ctx, p, "--failed", "--plain", "--no-legend", "--no-pager").Output()
	units := parseFailedUnits(string(out))
	list := "none"
	if len(units) > 0 {
		list = strings.Join(units, ", ")
	}
	return []model.Metric{
		{Key: "systemd_failed_units", Name: "Failed units", Value: len(units), StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:alert-circle"},
		{Key: "systemd_failed_list", Name: "Failed units list", Value: list, Kind: model.KindText, Category: "diagnostic"},
	}, nil
}
