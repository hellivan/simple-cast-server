// Package config loads the list of castable devices from a YAML file.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Device describes a single Chromecast/Nest Hub target that can be
// controlled by simple-cast-server.
type Device struct {
	// ID uniquely identifies the device and is used to build its MQTT
	// topics (e.g. "nesthub-1").
	ID string `yaml:"id"`
	// Target is the URL that will be cast to the device (e.g. an
	// immich-kiosk instance/display URL).
	Target string `yaml:"target"`
	// IP is the Chromecast device's IP address on the local network.
	IP string `yaml:"ip"`
	// Port is the Cast V2 control port. Defaults to 8009 when 0.
	Port int `yaml:"port"`
}

// Devices is the top-level config document: a plain list of devices.
type Devices []Device

// Load reads and parses the YAML device list at path.
func Load(path string) (Devices, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var devices Devices
	if err := yaml.Unmarshal(data, &devices); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	if err := devices.validate(); err != nil {
		return nil, err
	}

	return devices, nil
}

func (d Devices) validate() error {
	if len(d) == 0 {
		return fmt.Errorf("config: no devices defined")
	}

	seen := make(map[string]bool, len(d))
	for i, dev := range d {
		if dev.ID == "" {
			return fmt.Errorf("config: device #%d is missing an id", i)
		}
		if seen[dev.ID] {
			return fmt.Errorf("config: duplicate device id %q", dev.ID)
		}
		seen[dev.ID] = true

		if dev.Target == "" {
			return fmt.Errorf("config: device %q is missing target", dev.ID)
		}
		if dev.IP == "" {
			return fmt.Errorf("config: device %q is missing ip", dev.ID)
		}
	}
	return nil
}
