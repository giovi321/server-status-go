// Package sink renders snapshots to output transports.
package sink

import "github.com/giovi321/server-status/internal/model"

// Sink is one output transport. Phase 1 provides MQTT.
type Sink interface {
	Connect() error
	Publish(snap model.Snapshot) error
	Close() error
}
