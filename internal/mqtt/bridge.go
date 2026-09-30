// Package mqtt bridges Home Assistant (via MQTT + MQTT Discovery) and one or
// more Chromecast/Nest Hub devices controlled through the chromecast
// package. Each configured device is exposed to Home Assistant as a
// "switch" entity: turning it on casts the device's configured target URL,
// turning it off stops casting, and the switch state reflects whether
// DashCast is currently the foreground app on the device.
package mqtt

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/hellivan/simple-cast-server/internal/chromecast"
	"github.com/hellivan/simple-cast-server/internal/config"
)

const (
	payloadOn    = "ON"
	payloadOff   = "OFF"
	payloadOnl   = "online"
	payloadOffl  = "offline"
	qosAtLeast   = 1
	connectDelay = 2 * time.Second
)

// Options configures the MQTT bridge.
type Options struct {
	// Broker is the MQTT broker URL, e.g. "tcp://localhost:1883".
	Broker string
	// Username/Password authenticate against the broker. Optional.
	Username string
	Password string
	// ClientID is the MQTT client identifier.
	ClientID string
	// TopicPrefix namespaces this bridge's state/command topics, e.g.
	// "simple-cast-server".
	TopicPrefix string
	// DiscoveryPrefix is Home Assistant's MQTT discovery prefix, normally
	// "homeassistant".
	DiscoveryPrefix string
	// PollInterval controls how often each device's real Chromecast status
	// is polled and republished.
	PollInterval time.Duration
}

// Bridge owns the MQTT connection and the per-device Chromecast clients.
type Bridge struct {
	opts    Options
	devices config.Devices
	log     *slog.Logger

	client paho.Client

	mu      sync.Mutex
	casters map[string]*chromecast.Client
}

// New creates a Bridge for the given devices. Call Run to connect and start
// serving.
func New(devices config.Devices, opts Options, log *slog.Logger) *Bridge {
	if log == nil {
		log = slog.Default()
	}
	if opts.TopicPrefix == "" {
		opts.TopicPrefix = "simple-cast-server"
	}
	if opts.DiscoveryPrefix == "" {
		opts.DiscoveryPrefix = "homeassistant"
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 15 * time.Second
	}
	if opts.ClientID == "" {
		opts.ClientID = "simple-cast-server"
	}

	b := &Bridge{
		opts:    opts,
		devices: devices,
		log:     log,
		casters: make(map[string]*chromecast.Client, len(devices)),
	}
	for _, d := range devices {
		b.casters[d.ID] = chromecast.NewClient(d.IP, d.Port, log.With("device", d.ID))
	}
	return b
}

func (b *Bridge) bridgeStatusTopic() string {
	return fmt.Sprintf("%s/bridge/status", b.opts.TopicPrefix)
}

func (b *Bridge) stateTopic(id string) string {
	return fmt.Sprintf("%s/%s/state", b.opts.TopicPrefix, id)
}

func (b *Bridge) commandTopic(id string) string {
	return fmt.Sprintf("%s/%s/set", b.opts.TopicPrefix, id)
}

func (b *Bridge) discoveryTopic(id string) string {
	return fmt.Sprintf("%s/switch/%s_%s/config", b.opts.DiscoveryPrefix, b.opts.TopicPrefix, id)
}

// Run connects to the broker and blocks, serving MQTT traffic and polling
// device status, until stop is closed.
func (b *Bridge) Run(stop <-chan struct{}) error {
	connOpts := paho.NewClientOptions().
		AddBroker(b.opts.Broker).
		SetClientID(b.opts.ClientID).
		SetAutoReconnect(true).
		SetOrderMatters(false).
		SetWill(b.bridgeStatusTopic(), payloadOffl, qosAtLeast, true).
		SetOnConnectHandler(b.onConnect).
		SetConnectionLostHandler(func(_ paho.Client, err error) {
			b.log.Warn("mqtt: connection lost", "error", err)
		})
	if b.opts.Username != "" {
		connOpts.SetUsername(b.opts.Username)
	}
	if b.opts.Password != "" {
		connOpts.SetPassword(b.opts.Password)
	}

	b.client = paho.NewClient(connOpts)
	token := b.client.Connect()
	if token.Wait(); token.Error() != nil {
		return fmt.Errorf("mqtt: connect: %w", token.Error())
	}

	pollStop := make(chan struct{})
	var wg sync.WaitGroup
	for _, d := range b.devices {
		wg.Add(1)
		go func(d config.Device) {
			defer wg.Done()
			b.pollLoop(d, pollStop)
		}(d)
	}

	<-stop
	close(pollStop)
	wg.Wait()

	b.publish(b.bridgeStatusTopic(), payloadOffl, true)
	b.client.Disconnect(uint(connectDelay.Milliseconds()))
	return nil
}

// onConnect (re-)publishes discovery configs, subscribes to command topics
// and announces the bridge as available. It is called on every successful
// (re)connection so Home Assistant discovery survives broker restarts.
func (b *Bridge) onConnect(_ paho.Client) {
	b.log.Info("mqtt: connected", "broker", b.opts.Broker)

	for _, d := range b.devices {
		b.publishDiscovery(d)

		device := d
		topic := b.commandTopic(device.ID)
		token := b.client.Subscribe(topic, qosAtLeast, func(_ paho.Client, msg paho.Message) {
			b.handleCommand(device, string(msg.Payload()))
		})
		token.Wait()
		if err := token.Error(); err != nil {
			b.log.Error("mqtt: failed to subscribe", "topic", topic, "error", err)
		}
	}

	b.publish(b.bridgeStatusTopic(), payloadOnl, true)
}

func (b *Bridge) publish(topic, payload string, retain bool) {
	if b.client == nil || !b.client.IsConnected() {
		return
	}
	token := b.client.Publish(topic, qosAtLeast, retain, payload)
	token.Wait()
	if err := token.Error(); err != nil {
		b.log.Error("mqtt: publish failed", "topic", topic, "error", err)
	}
}

func (b *Bridge) caster(id string) *chromecast.Client {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.casters[id]
}

func (b *Bridge) handleCommand(d config.Device, payload string) {
	caster := b.caster(d.ID)
	if caster == nil {
		return
	}

	var err error
	switch payload {
	case payloadOn:
		b.log.Info("mqtt: starting cast", "device", d.ID, "target", d.Target)
		err = caster.Start(d.Target)
	case payloadOff:
		b.log.Info("mqtt: stopping cast", "device", d.ID)
		err = caster.Stop()
	default:
		b.log.Warn("mqtt: ignoring unknown command payload", "device", d.ID, "payload", payload)
		return
	}
	if err != nil {
		b.log.Error("mqtt: command failed", "device", d.ID, "payload", payload, "error", err)
		return
	}

	// Publish immediately for a snappy UI; the poll loop will correct this
	// if reality differs.
	if payload == payloadOn {
		b.publish(b.stateTopic(d.ID), payloadOn, true)
	} else {
		b.publish(b.stateTopic(d.ID), payloadOff, true)
	}
}

func (b *Bridge) pollLoop(d config.Device, stop <-chan struct{}) {
	caster := b.caster(d.ID)
	if caster == nil {
		return
	}

	ticker := time.NewTicker(b.opts.PollInterval)
	defer ticker.Stop()

	poll := func() {
		state, err := caster.Status()
		if err != nil {
			b.log.Debug("mqtt: status poll failed", "device", d.ID, "error", err)
			return
		}
		payload := payloadOff
		if state.Casting {
			payload = payloadOn
		}
		b.publish(b.stateTopic(d.ID), payload, true)
	}

	poll()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			poll()
		}
	}
}

// publishDiscovery publishes a Home Assistant MQTT discovery config for a
// "switch" entity representing device d.
func (b *Bridge) publishDiscovery(d config.Device) {
	uniqueID := fmt.Sprintf("%s_%s", b.opts.TopicPrefix, d.ID)

	discoveryPayload := map[string]any{
		"name":                  nil, // use the device name as the entity name
		"unique_id":             uniqueID,
		"object_id":             uniqueID,
		"state_topic":           b.stateTopic(d.ID),
		"command_topic":         b.commandTopic(d.ID),
		"payload_on":            payloadOn,
		"payload_off":           payloadOff,
		"state_on":              payloadOn,
		"state_off":             payloadOff,
		"availability_topic":    b.bridgeStatusTopic(),
		"payload_available":     payloadOnl,
		"payload_not_available": payloadOffl,
		"icon":                  "mdi:cast",
		"device": map[string]any{
			"identifiers":  []string{uniqueID},
			"name":         d.ID,
			"manufacturer": "simple-cast-server",
			"model":        "DashCast bridge",
		},
	}

	data, err := json.Marshal(discoveryPayload)
	if err != nil {
		b.log.Error("mqtt: failed to marshal discovery payload", "device", d.ID, "error", err)
		return
	}

	b.publish(b.discoveryTopic(d.ID), string(data), true)
}
