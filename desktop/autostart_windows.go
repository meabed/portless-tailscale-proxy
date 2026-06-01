//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// HKCU Run key — the per-user "start at login" registry location.
const (
	autostartRunKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	autostartName   = "TailscaleProxy"
)

func hiddenReg(args ...string) *exec.Cmd {
	cmd := exec.Command("reg", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func autostartEnabled() bool {
	return hiddenReg("query", autostartRunKey, "/v", autostartName).Run() == nil
}

func enableAutostart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return hiddenReg("add", autostartRunKey, "/v", autostartName, "/t", "REG_SZ", "/d", exe, "/f").Run()
}

func disableAutostart() error {
	// /f makes delete idempotent; ignore the "value not found" exit.
	_ = hiddenReg("delete", autostartRunKey, "/v", autostartName, "/f").Run()
	return nil
}
