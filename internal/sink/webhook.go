package sink

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
)

// Webhook POSTs the normalized snapshot JSON to a URL. It consumes the same
// model.Snapshot as the MQTT sink, so the two are metric-for-metric identical.
type Webhook struct {
	sc          config.SinkConfig
	client      *http.Client
	lastMetrics string
}

// NewWebhook builds a webhook sink.
func NewWebhook(sc config.SinkConfig) *Webhook {
	return &Webhook{sc: sc, client: &http.Client{Timeout: 10 * time.Second}}
}

// Connect is a no-op; the webhook sink is connectionless.
func (w *Webhook) Connect() error { return nil }

// Publish POSTs the snapshot as JSON with retry. With on_change it skips a POST
// when the metrics are unchanged since the last publish (ignoring the timestamp).
func (w *Webhook) Publish(snap model.Snapshot) error {
	if w.sc.OnChange {
		mj, _ := json.Marshal(snap.Metrics)
		if string(mj) == w.lastMetrics {
			return nil
		}
		w.lastMetrics = string(mj)
	}
	body, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest(http.MethodPost, w.sc.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if w.sc.Token != "" {
			req.Header.Set("Authorization", "Bearer "+w.sc.Token)
		}
		resp, err := w.client.Do(req)
		if err == nil {
			code := resp.StatusCode
			resp.Body.Close()
			if code < 300 {
				return nil
			}
			lastErr = fmt.Errorf("webhook POST to %s returned %d", w.sc.URL, code)
		} else {
			lastErr = err
		}
		time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
	}
	return lastErr
}

// Close is a no-op.
func (w *Webhook) Close() error { return nil }
