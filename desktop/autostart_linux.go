//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func autostartDesktopPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", "tailscale-proxy.desktop"), nil
}

func autostartEnabled() bool {
	p, err := autostartDesktopPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func enableAutostart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	p, err := autostartDesktopPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Tailscale Proxy
Exec=%s
X-GNOME-Autostart-enabled=true
Terminal=false
`, exe)
	return os.WriteFile(p, []byte(entry), 0o644)
}

func disableAutostart() error {
	p, err := autostartDesktopPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
