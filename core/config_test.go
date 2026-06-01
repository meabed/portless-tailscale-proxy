package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Ports != "3000-5000" {
		t.Errorf("Ports = %q, want %q", cfg.Ports, "3000-5000")
	}
	if cfg.Port != 8443 {
		t.Errorf("Port = %d, want 8443", cfg.Port)
	}
	if cfg.Interval != 20 {
		t.Errorf("Interval = %d, want 20", cfg.Interval)
	}
	if cfg.HTTPSPort != 443 {
		t.Errorf("HTTPSPort = %d, want 443", cfg.HTTPSPort)
	}
	if !cfg.LogRequests {
		t.Error("LogRequests = false, want true")
	}
	if cfg.DeregisterCycles != 5 {
		t.Errorf("DeregisterCycles = %d, want 5", cfg.DeregisterCycles)
	}
	if cfg.All {
		t.Error("All = true, want false")
	}
	if cfg.Private {
		t.Error("Private = true, want false")
	}
}

func TestLoadConfigFrom_missingReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent", "config.json")

	cfg, existed, err := loadConfigFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if existed {
		t.Error("existed = true, want false")
	}
	want := defaultConfig()
	if cfg != want {
		t.Errorf("cfg = %+v, want %+v", cfg, want)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	original := Config{
		Ports:            "4000-4999",
		All:              true,
		Runtimes:         "node,bun",
		Private:          true,
		Port:             9443,
		Interval:         30,
		HTTPSPort:        8443,
		LogRequests:      false,
		DeregisterCycles: 10,
	}

	if err := saveConfigTo(path, original); err != nil {
		t.Fatalf("saveConfigTo error: %v", err)
	}

	loaded, existed, err := loadConfigFrom(path)
	if err != nil {
		t.Fatalf("loadConfigFrom error: %v", err)
	}
	if !existed {
		t.Error("existed = false, want true")
	}
	if loaded != original {
		t.Errorf("loaded = %+v, want %+v", loaded, original)
	}
}

func TestLoadConfigFrom_partialOverlaysDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write only the port field.
	partial := map[string]interface{}{"port": 9000}
	data, err := json.Marshal(partial)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, existed, err := loadConfigFrom(path)
	if err != nil {
		t.Fatalf("loadConfigFrom error: %v", err)
	}
	if !existed {
		t.Error("existed = false, want true")
	}
	if cfg.Port != 9000 {
		t.Errorf("Port = %d, want 9000", cfg.Port)
	}
	// Unspecified fields must still hold their default values.
	def := defaultConfig()
	if cfg.Interval != def.Interval {
		t.Errorf("Interval = %d, want %d", cfg.Interval, def.Interval)
	}
	if cfg.DeregisterCycles != def.DeregisterCycles {
		t.Errorf("DeregisterCycles = %d, want %d", cfg.DeregisterCycles, def.DeregisterCycles)
	}
	if cfg.Ports != def.Ports {
		t.Errorf("Ports = %q, want %q", cfg.Ports, def.Ports)
	}
	if cfg.HTTPSPort != def.HTTPSPort {
		t.Errorf("HTTPSPort = %d, want %d", cfg.HTTPSPort, def.HTTPSPort)
	}
	if cfg.LogRequests != def.LogRequests {
		t.Errorf("LogRequests = %v, want %v", cfg.LogRequests, def.LogRequests)
	}
}
