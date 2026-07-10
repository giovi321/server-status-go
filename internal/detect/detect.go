// Package detect wires the built-in collectors together and powers --dump-detected.
package detect

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/giovi321/server-status/internal/collector"
	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
)

type collectorIface = collector.Collector

// All returns every built-in collector, regardless of availability.
func All(cfg config.Config) []collector.Collector {
	return []collector.Collector{
		collector.CPU{},
		collector.Memory{},
		collector.Uptime{},
		collector.Load{},
		collector.Swap{},
		collector.Filesystem{},
		&collector.Network{},
		collector.Temperature{},
		collector.Apt{},
		collector.Systemd{},
		collector.NewSmart(cfg),
		collector.Mdadm{},
		collector.Gpu{},
		collector.Zfs{},
		collector.NewDocker(),
		collector.NewRsnapshot(cfg),
		collector.Agent{},
	}
}

func availableFrom(cols []collectorIface) []collectorIface {
	var out []collectorIface
	for _, c := range cols {
		if c.Available() {
			out = append(out, c)
		}
	}
	return out
}

// Available returns only the collectors that report data on this host.
func Available(cols []collector.Collector) []collector.Collector {
	return availableFrom(cols)
}

// defaultCollectTimeout bounds a single collector's Collect call. Collectors run
// serially, so this is sized well under the systemd WatchdogSec (180s) even when
// several collectors hit their timeout in the same cycle.
const defaultCollectTimeout = 20 * time.Second

// collectSafe runs one collector with a timeout and isolates panics, so a slow
// or panicking collector cannot stall the cycle or crash the agent. Collectors
// run serially (they hold per-instance state), and all respect ctx cancellation.
func collectSafe(ctx context.Context, c collectorIface, timeout time.Duration) (out []model.Metric) {
	defer func() {
		if r := recover(); r != nil {
			out = nil
		}
	}()
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	m, err := c.Collect(cctx)
	if err != nil {
		return nil
	}
	return m
}

func snapshotFrom(ctx context.Context, dev model.Device, cols []collectorIface) model.Snapshot {
	snap := model.Snapshot{Device: dev, TS: time.Now()}
	for _, c := range cols {
		snap.Metrics = append(snap.Metrics, collectSafe(ctx, c, defaultCollectTimeout)...)
	}
	return snap
}

// Snapshot runs the given collectors and aggregates their metrics.
func Snapshot(ctx context.Context, dev model.Device, cols []collector.Collector) model.Snapshot {
	return snapshotFrom(ctx, dev, cols)
}

type dumpCollector struct {
	Name      string         `json:"name"`
	Available bool           `json:"available"`
	Metrics   []model.Metric `json:"metrics,omitempty"`
}

// DumpJSON writes an indented JSON report of each collector and the metrics it would publish.
func DumpJSON(w io.Writer, dev model.Device, cols []collector.Collector, ctx context.Context) error {
	report := struct {
		Device     model.Device    `json:"device"`
		Collectors []dumpCollector `json:"collectors"`
	}{Device: dev}
	for _, c := range cols {
		dc := dumpCollector{Name: c.Name(), Available: c.Available()}
		if dc.Available {
			if metrics, err := c.Collect(ctx); err == nil {
				dc.Metrics = metrics
			}
		}
		report.Collectors = append(report.Collectors, dc)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
