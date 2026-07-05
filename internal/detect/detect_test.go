package detect

import (
	"context"
	"testing"

	"github.com/giovi321/server-status/internal/model"
)

// fake is a deterministic collector for testing aggregation without touching /proc.
type fake struct {
	name    string
	avail   bool
	metrics []model.Metric
}

func (f fake) Name() string                                    { return f.name }
func (f fake) Available() bool                                 { return f.avail }
func (f fake) Collect(context.Context) ([]model.Metric, error) { return f.metrics, nil }

func TestAvailableFilters(t *testing.T) {
	cols := []collectorIface{
		fake{name: "a", avail: true},
		fake{name: "b", avail: false},
	}
	got := availableFrom(cols)
	if len(got) != 1 || got[0].Name() != "a" {
		t.Fatalf("got %d collectors", len(got))
	}
}

func TestSnapshotAggregates(t *testing.T) {
	cols := []collectorIface{
		fake{name: "a", avail: true, metrics: []model.Metric{{Key: "x"}}},
		fake{name: "b", avail: true, metrics: []model.Metric{{Key: "y"}, {Key: "z"}}},
	}
	snap := snapshotFrom(context.Background(), model.Device{Node: "n"}, cols)
	if len(snap.Metrics) != 3 {
		t.Fatalf("got %d metrics", len(snap.Metrics))
	}
	if snap.TS.IsZero() {
		t.Fatal("timestamp not set")
	}
}

type panicky struct{ name string }

func (p panicky) Name() string                                    { return p.name }
func (p panicky) Available() bool                                 { return true }
func (p panicky) Collect(context.Context) ([]model.Metric, error) { panic("boom") }

func TestSnapshotIsolatesPanic(t *testing.T) {
	cols := []collectorIface{
		fake{name: "ok", avail: true, metrics: []model.Metric{{Key: "x"}}},
		panicky{name: "bad"},
		fake{name: "ok2", avail: true, metrics: []model.Metric{{Key: "y"}}},
	}
	// Must not panic, and must still collect from the healthy collectors.
	snap := snapshotFrom(context.Background(), model.Device{Node: "n"}, cols)
	keys := map[string]bool{}
	for _, m := range snap.Metrics {
		keys[m.Key] = true
	}
	if !keys["x"] || !keys["y"] {
		t.Fatalf("healthy collectors must still run despite a panicking one: %+v", keys)
	}
}
