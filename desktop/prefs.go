package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// prefs holds desktop-only settings (kept separate from the shared CLI config).
type prefs struct {
	HideDock bool `json:"hideDock"`
}

func defaultPrefs() prefs { return prefs{HideDock: true} } // tray-first by default

func prefsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tailscale-proxy", "desktop.json"), nil
}

func loadPrefs() prefs {
	p := defaultPrefs()
	path, err := prefsPath()
	if err != nil {
		return p
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &p)
	}
	return p
}

func savePrefs(p prefs) error {
	path, err := prefsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(p, "", "  ")
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
