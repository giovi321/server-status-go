package collector

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// parseAptUpgradable counts "Inst " lines from `apt-get -s dist-upgrade`.
// A line whose new-version archive mentions "security" is also counted as a security update.
func parseAptUpgradable(out string) (total, security int) {
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "Inst ") {
			continue
		}
		total++
		if strings.Contains(strings.ToLower(line), "security") {
			security++
		}
	}
	return total, security
}

func aptPath() string {
	for _, p := range []string{"/usr/bin/apt-get", "/bin/apt-get"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Apt publishes upgradable package counts and the reboot-required flag.
type Apt struct{}

func (Apt) Name() string { return "apt" }

func (Apt) Available() bool { return aptPath() != "" }

func (Apt) Collect(ctx context.Context) ([]model.Metric, error) {
	p := aptPath()
	if p == "" {
		return nil, nil
	}
	cmd := exec.CommandContext(ctx, p, "-s", "dist-upgrade")
	out, _ := cmd.Output() // non-zero exit still yields parseable stdout
	total, sec := parseAptUpgradable(string(out))
	_, rebootErr := os.Stat("/var/run/reboot-required")
	rebootRequired := rebootErr == nil
	return []model.Metric{
		{Key: "apt_updates", Name: "APT updates", Value: total, StateClass: "total", Kind: model.KindSensor, Category: "primary", Icon: "mdi:package-up"},
		{Key: "apt_security_updates", Name: "APT security updates", Value: sec, StateClass: "total", Kind: model.KindSensor, Category: "primary", Icon: "mdi:shield-alert"},
		{Key: "reboot_required", Name: "Reboot required", Value: rebootRequired, DeviceClass: "update", Kind: model.KindBinarySensor, Category: "primary"},
	}, nil
}
