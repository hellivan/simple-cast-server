package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadValid(t *testing.T) {
	path := writeConfig(t, `
- id: nesthub-1
  target: https://immich-kiosk.example.com
  ip: 10.0.0.4
- id: nesthub-2
  target: https://immich-kiosk.example.com/display/2
  ip: 10.0.0.5
  port: 8009
`)

	devices, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("len(devices) = %d, want 2", len(devices))
	}
	if devices[0].ID != "nesthub-1" || devices[0].IP != "10.0.0.4" {
		t.Errorf("unexpected device[0]: %+v", devices[0])
	}
}

func TestLoadRejectsDuplicateID(t *testing.T) {
	path := writeConfig(t, `
- id: nesthub-1
  target: https://a.example.com
  ip: 10.0.0.4
- id: nesthub-1
  target: https://b.example.com
  ip: 10.0.0.5
`)

	if _, err := Load(path); err == nil {
		t.Error("expected error for duplicate id, got nil")
	}
}

func TestLoadRejectsMissingFields(t *testing.T) {
	cases := []string{
		`- target: https://a.example.com
  ip: 10.0.0.4`,
		`- id: nesthub-1
  ip: 10.0.0.4`,
		`- id: nesthub-1
  target: https://a.example.com`,
	}
	for _, c := range cases {
		path := writeConfig(t, c)
		if _, err := Load(path); err == nil {
			t.Errorf("expected error for config %q, got nil", c)
		}
	}
}

func TestLoadRejectsEmptyList(t *testing.T) {
	path := writeConfig(t, `[]`)
	if _, err := Load(path); err == nil {
		t.Error("expected error for empty device list, got nil")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load("/nonexistent/devices.yaml"); err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
