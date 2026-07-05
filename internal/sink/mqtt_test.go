package sink

import (
	"testing"

	"github.com/giovi321/server-status/internal/model"
)

func TestDiscoveryDedupKeyIncludesInstance(t *testing.T) {
	a := discoveryDedupKey(model.Metric{Key: "fs_usage", Instance: "root"})
	b := discoveryDedupKey(model.Metric{Key: "fs_usage", Instance: "mnt-z"})
	if a == b {
		t.Fatalf("multi-instance metrics must have distinct dedup keys; both = %q", a)
	}
	// same key+component+instance must dedup to the same value
	c := discoveryDedupKey(model.Metric{Key: "cpu_usage"})
	d := discoveryDedupKey(model.Metric{Key: "cpu_usage"})
	if c != d {
		t.Fatalf("identical metrics must share a dedup key: %q vs %q", c, d)
	}
}
