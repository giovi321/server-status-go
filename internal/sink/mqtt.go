package sink

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/giovi321/server-status/internal/command"
	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/ha"
	"github.com/giovi321/server-status/internal/model"
)

// MQTT publishes snapshots to an MQTT broker with Home Assistant discovery.
type MQTT struct {
	sc         config.SinkConfig
	dev        model.Device
	client     mqtt.Client
	availTopic string
	disp       *command.Dispatcher
	mu         sync.Mutex
	// discovered tracks which metric keys have had discovery published this connection.
	discovered map[string]bool
}

// discoveryDedupKey identifies a distinct discovered entity. It MUST include
// Instance: multi-instance metrics (filesystems, network, temperatures) share a
// Key, and omitting Instance would publish discovery for only the first instance.
func discoveryDedupKey(m model.Metric) string {
	return m.Key + "|" + m.Component + "|" + m.Instance
}

// NewMQTT builds an unconnected MQTT sink. disp is optional (nil disables
// command subscription and button/update discovery).
func NewMQTT(sc config.SinkConfig, dev model.Device, disp *command.Dispatcher) *MQTT {
	return &MQTT{
		sc:         sc,
		dev:        dev,
		disp:       disp,
		availTopic: ha.AvailabilityTopic(sc.BaseTopic, dev.Node),
		discovered: map[string]bool{},
	}
}

// Connect establishes the broker connection, sets the LWT, and publishes availability online.
func (m *MQTT) Connect() error {
	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%d", m.sc.Host, m.sc.Port)).
		SetClientID("server-status-"+m.dev.Node).
		SetKeepAlive(30*time.Second).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(60*time.Second).
		SetWill(m.availTopic, "offline", byte(m.sc.QoS), true)
	if m.sc.Username != "" {
		opts.SetUsername(m.sc.Username).SetPassword(m.sc.Password)
	}
	// On every (re)connect, republish availability and force discovery to be re-sent.
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		m.mu.Lock()
		m.discovered = map[string]bool{}
		m.mu.Unlock()
		c.Publish(m.availTopic, byte(m.sc.QoS), true, "online")
		if m.disp != nil {
			c.Subscribe(m.sc.BaseTopic+"/"+m.dev.Node+"/cmd/+", byte(m.sc.QoS), func(_ mqtt.Client, msg mqtt.Message) {
				if msg.Retained() {
					return
				}
				parts := strings.Split(msg.Topic(), "/")
				name := parts[len(parts)-1]
				res := m.disp.Run(context.Background(), name)
				body, _ := json.Marshal(res)
				m.client.Publish(m.sc.BaseTopic+"/"+m.dev.Node+"/cmd/"+name+"/result", byte(m.sc.QoS), false, body)
			})
		}
	})

	m.client = mqtt.NewClient(opts)
	tok := m.client.Connect()
	if !tok.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("mqtt connect timeout to %s:%d", m.sc.Host, m.sc.Port)
	}
	return tok.Error()
}

// Publish sends discovery (once per connection per metric) then the current state for each metric.
func (m *MQTT) Publish(snap model.Snapshot) error {
	if m.client == nil || !m.client.IsConnected() {
		return fmt.Errorf("mqtt sink not connected; skipping publish for %s", snap.Device.Node)
	}
	if m.disp != nil {
		if err := m.publishControlDiscoveryOnce(snap.Device); err != nil {
			return err
		}
	}
	for _, metric := range snap.Metrics {
		key := discoveryDedupKey(metric)
		m.mu.Lock()
		already := m.discovered[key]
		m.mu.Unlock()
		if !already {
			topic, payload, err := ha.Discovery(snap.Device, metric, m.sc)
			if err != nil {
				return err
			}
			m.client.Publish(topic, byte(m.sc.QoS), true, payload)
			m.mu.Lock()
			m.discovered[key] = true
			m.mu.Unlock()
		}
		stateTopic := ha.StateTopic(m.sc.BaseTopic, snap.Device.Node, metric.Component, metric.Key, metric.Instance)
		m.client.Publish(stateTopic, byte(m.sc.QoS), m.sc.Retain, ha.StateValue(metric))
	}
	return nil
}

// publishControlDiscoveryOnce publishes the command button and update entity
// discovery, once per connection, guarded by the same discovered map used for
// metric discovery (synthetic keys so they never collide with metric keys).
func (m *MQTT) publishControlDiscoveryOnce(dev model.Device) error {
	type entry struct {
		key   string
		build func() (string, []byte, error)
	}
	entries := []entry{
		{key: "cmd|refresh", build: func() (string, []byte, error) { return ha.ButtonDiscovery(dev, m.sc, "refresh", "Refresh") }},
		{key: "cmd|restart", build: func() (string, []byte, error) { return ha.ButtonDiscovery(dev, m.sc, "restart", "Restart") }},
		{key: "cmd|update", build: func() (string, []byte, error) { return ha.UpdateDiscovery(dev, m.sc) }},
	}
	for _, e := range entries {
		m.mu.Lock()
		already := m.discovered[e.key]
		m.mu.Unlock()
		if already {
			continue
		}
		topic, payload, err := e.build()
		if err != nil {
			return err
		}
		m.client.Publish(topic, byte(m.sc.QoS), true, payload)
		m.mu.Lock()
		m.discovered[e.key] = true
		m.mu.Unlock()
	}
	return nil
}

// Purge clears all retained discovery for this host so Home Assistant removes the
// device and every entity — including entities from earlier cycles that are absent
// from the current snapshot (e.g. an unplugged disk or a stopped container). It
// sweeps every retained discovery config topic under this node and clears them,
// also clears the current snapshot's entities and control entities explicitly (in
// case retained delivery is slow), clears the availability topic, then disconnects
// gracefully so the LWT does not republish an offline availability.
func (m *MQTT) Purge(snap model.Snapshot) error {
	if m.client == nil || !m.client.IsConnected() {
		return fmt.Errorf("mqtt sink not connected; cannot purge %s", snap.Device.Node)
	}
	clear := func(topic string) {
		m.client.Publish(topic, byte(m.sc.QoS), true, "").WaitTimeout(2 * time.Second)
	}

	// 1) Sweep: collect every retained discovery config topic for this node. The
	//    node segment is always dev.Node, so this wildcard matches host and
	//    sub-device entities alike, including ones no longer in the snapshot.
	wildcard := m.sc.DiscoveryPrefix + "/+/" + m.dev.Node + "/+/config"
	var mu sync.Mutex
	swept := map[string]bool{}
	tok := m.client.Subscribe(wildcard, byte(m.sc.QoS), func(_ mqtt.Client, msg mqtt.Message) {
		if len(msg.Payload()) == 0 {
			return // already cleared
		}
		mu.Lock()
		swept[msg.Topic()] = true
		mu.Unlock()
	})
	tok.WaitTimeout(2 * time.Second)
	time.Sleep(2 * time.Second) // let the broker deliver retained configs
	m.client.Unsubscribe(wildcard).WaitTimeout(2 * time.Second)
	mu.Lock()
	topics := make([]string, 0, len(swept))
	for t := range swept {
		topics = append(topics, t)
	}
	mu.Unlock()
	for _, t := range topics {
		clear(t)
	}

	// 2) Belt-and-suspenders: clear the current snapshot's entities + control
	//    entities explicitly, in case retained delivery did not surface them.
	for _, metric := range snap.Metrics {
		if topic, _, err := ha.Discovery(snap.Device, metric, m.sc); err == nil {
			clear(topic)
		}
	}
	if m.disp != nil {
		if t, _, err := ha.ButtonDiscovery(snap.Device, m.sc, "refresh", "Refresh"); err == nil {
			clear(t)
		}
		if t, _, err := ha.ButtonDiscovery(snap.Device, m.sc, "restart", "Restart"); err == nil {
			clear(t)
		}
		if t, _, err := ha.UpdateDiscovery(snap.Device, m.sc); err == nil {
			clear(t)
		}
	}

	// 3) Availability last, then graceful disconnect (no LWT republish).
	clear(m.availTopic)
	m.client.Disconnect(250)
	return nil
}

// Close publishes offline and disconnects.
func (m *MQTT) Close() error {
	if m.client == nil {
		return nil
	}
	if m.client.IsConnected() {
		tok := m.client.Publish(m.availTopic, byte(m.sc.QoS), true, "offline")
		tok.WaitTimeout(2 * time.Second)
	}
	m.client.Disconnect(250)
	return nil
}
