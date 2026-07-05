package collector

import (
	"context"
	"testing"

	"github.com/giovi321/server-status/internal/model"
)

func TestAgentMetrics(t *testing.T) {
	ms, err := Agent{}.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]model.Metric{}
	for _, m := range ms {
		by[m.Key] = m
	}
	ls, ok := by["last_seen"]
	if !ok {
		t.Fatal("missing last_seen")
	}
	if ls.DeviceClass != "timestamp" || ls.Category != "diagnostic" {
		t.Fatalf("last_seen must be a diagnostic timestamp: %+v", ls)
	}
	if _, ok := by["agent_version"]; !ok {
		t.Fatal("missing agent_version")
	}
}
