package sink

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
// when the metrics are unchanged since the last SUCCESSFUL publish (ignoring the
// timestamp), so a failed delivery is retried on the next cycle rather than lost.
func (w *Webhook) Publish(snap model.Snapshot) error {
	var mj string
	if w.sc.OnChange {
		b, _ := json.Marshal(snap.Metrics)
		mj = string(b)
		if mj == w.lastMetrics {
			return nil
		}
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
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if code < 300 {
				if w.sc.OnChange {
					w.lastMetrics = mj // commit only after successful delivery
				}
				return nil
			}
			lastErr = fmt.Errorf("webhook POST to %s returned %d", w.sc.URL, code)
		} else {
			lastErr = err
		}
		if attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
		}
	}
	return lastErr
}

// Close is a no-op.
func (w *Webhook) Close() error { return nil }
