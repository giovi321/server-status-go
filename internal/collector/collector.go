// Package collector defines the Collector interface and the built-in collectors.
package collector

import (
	"context"

	"github.com/giovi321/server-status/internal/model"
)

// Collector produces zero or more metrics for one metric family.
type Collector interface {
	// Name is a stable family identifier, e.g. "cpu".
	Name() string
	// Available reports whether this host can produce these metrics.
	Available() bool
	// Collect gathers the current metrics.
	Collect(ctx context.Context) ([]model.Metric, error)
}
