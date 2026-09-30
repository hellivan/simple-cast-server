// Command simple-cast-server bridges Home Assistant (over MQTT) and one or
// more Chromecast / Google Nest Hub devices, letting Home Assistant start,
// stop and monitor casting a configured URL (e.g. an immich-kiosk display)
// via the DashCast receiver app - without needing catt or any Python
// dependency.
package main

import (
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hellivan/simple-cast-server/internal/config"
	"github.com/hellivan/simple-cast-server/internal/mqtt"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	var (
		configPath      string
		broker          string
		username        string
		password        string
		clientID        string
		topicPrefix     string
		discoveryPrefix string
		pollInterval    time.Duration
		logLevel        string
	)

	flag.StringVar(&configPath, "config", envOr("CONFIG_FILE", "/etc/simple-cast-server/devices.yaml"), "Path to the devices YAML config file (defaults to $CONFIG_FILE).")
	flag.StringVar(&broker, "mqtt-broker", envOr("MQTT_BROKER", "tcp://localhost:1883"), "MQTT broker URL (defaults to $MQTT_BROKER).")
	flag.StringVar(&username, "mqtt-username", envOr("MQTT_USERNAME", ""), "MQTT username (defaults to $MQTT_USERNAME).")
	flag.StringVar(&password, "mqtt-password", envOr("MQTT_PASSWORD", ""), "MQTT password (defaults to $MQTT_PASSWORD).")
	flag.StringVar(&clientID, "mqtt-client-id", envOr("MQTT_CLIENT_ID", "simple-cast-server"), "MQTT client id (defaults to $MQTT_CLIENT_ID).")
	flag.StringVar(&topicPrefix, "topic-prefix", envOr("TOPIC_PREFIX", "simple-cast-server"), "MQTT topic prefix for state/command topics (defaults to $TOPIC_PREFIX).")
	flag.StringVar(&discoveryPrefix, "discovery-prefix", envOr("DISCOVERY_PREFIX", "homeassistant"), "Home Assistant MQTT discovery prefix (defaults to $DISCOVERY_PREFIX).")
	flag.DurationVar(&pollInterval, "poll-interval", 15*time.Second, "How often to poll each device's real cast status.")
	flag.StringVar(&logLevel, "log-level", envOr("LOG_LEVEL", "info"), "Log level: debug, info, warn, error (defaults to $LOG_LEVEL).")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(logLevel)}))
	slog.SetDefault(logger)

	devices, err := config.Load(configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	logger.Info("loaded devices", "count", len(devices), "config", configPath)

	bridge := mqtt.New(devices, mqtt.Options{
		Broker:          broker,
		Username:        username,
		Password:        password,
		ClientID:        clientID,
		TopicPrefix:     topicPrefix,
		DiscoveryPrefix: discoveryPrefix,
		PollInterval:    pollInterval,
	}, logger)

	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		logger.Info("shutting down")
		close(stop)
	}()

	if err := bridge.Run(stop); err != nil {
		logger.Error("bridge stopped with error", "error", err)
		os.Exit(1)
	}
}

func parseLevel(level string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		return slog.LevelInfo
	}
	return l
}
