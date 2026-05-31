package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is the persisted tsp configuration. Zero values are NOT the defaults —
// always start from defaultConfig() and overlay the file on top.
type Config struct {
	Ports            string `json:"ports"`            // "3000-5000" or single "4000"
	All              bool   `json:"all"`              // include non-web runtimes
	Runtimes         string `json:"runtimes"`         // CSV, "" = all known
	Private          bool   `json:"private"`          // Serve (private) instead of Funnel
	Port             int    `json:"port"`             // local proxy HTTP port
	Interval         int    `json:"interval"`         // re-scan seconds
	HTTPSPort        int    `json:"httpsPort"`        // public/tailnet HTTPS port
	LogRequests      bool   `json:"logRequests"`      // per-request logging
	DeregisterCycles int    `json:"deregisterCycles"` // missing scans before removal
	ForwardHost      bool   `json:"forwardHost"`      // forward the external host to the app
}

// defaultConfig returns the built-in defaults.
func defaultConfig() Config {
	return Config{
		Ports: "3000-5000", All: false, Runtimes: "", Private: false,
		Port: 8443, Interval: 20, HTTPSPort: 443, LogRequests: true,
		DeregisterCycles: 5, ForwardHost: false,
	}
}

// configPath returns ~/.tailscale-proxy/config.json.
func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tailscale-proxy", "config.json"), nil
}

// loadConfigFrom reads a config file, overlaying it on defaults. Missing file →
// (defaults, false, nil). Returns (config, existed, error).
func loadConfigFrom(path string) (Config, bool, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, false, nil
		}
		return cfg, false, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, true, err
	}
	return cfg, true, nil
}

// saveConfigTo writes cfg as indented JSON, creating the parent dir.
func saveConfigTo(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// loadConfig loads the config at the default path. Returns (config, path, existed, error).
func loadConfig() (Config, string, bool, error) {
	path, err := configPath()
	if err != nil {
		return defaultConfig(), "", false, err
	}
	cfg, existed, err := loadConfigFrom(path)
	return cfg, path, existed, err
}

// saveConfig writes cfg to the default path and returns it.
func saveConfig(cfg Config) (string, error) {
	path, err := configPath()
	if err != nil {
		return "", err
	}
	return path, saveConfigTo(path, cfg)
}
