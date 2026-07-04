package sink

import (
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

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
	mu         sync.Mutex
	// discovered tracks which metric keys have had discovery published this connection.
	discovered map[string]bool
}

// NewMQTT builds an unconnected MQTT sink.
func NewMQTT(sc config.SinkConfig, dev model.Device) *MQTT {
	return &MQTT{
		sc:         sc,
		dev:        dev,
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
		SetConnectRetry(true).
		SetConnectRetryInterval(5*time.Second).
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
	for _, metric := range snap.Metrics {
		key := metric.Key + "|" + metric.Component
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
		stateTopic := ha.StateTopic(m.sc.BaseTopic, snap.Device.Node, metric.Component, metric.Key)
		m.client.Publish(stateTopic, byte(m.sc.QoS), m.sc.Retain, ha.StateValue(metric))
	}
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
