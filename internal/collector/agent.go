package collector

import (
	"context"
	"time"

	"github.com/giovi321/server-status/internal/model"
	"github.com/giovi321/server-status/internal/version"
)

// Agent publishes agent self-diagnostics: a last_seen heartbeat and the version.
type Agent struct{}

func (Agent) Name() string { return "agent" }

func (Agent) Available() bool { return true }

func (Agent) Collect(ctx context.Context) ([]model.Metric, error) {
	return []model.Metric{
		{Key: "last_seen", Name: "Last seen", Value: time.Now().UTC().Format(time.RFC3339), DeviceClass: "timestamp", Kind: model.KindSensor, Category: "diagnostic", Icon: "mdi:clock-check"},
		{Key: "agent_version", Name: "Agent version", Value: version.Version, Kind: model.KindText, Category: "diagnostic", Icon: "mdi:tag"},
	}, nil
}
