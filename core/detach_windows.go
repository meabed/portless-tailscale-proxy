//go:build windows

package core

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
}

// spawnDetached re-execs tsp without --bg, detached, with output to logPath.
func spawnDetached(logPath string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(exe, childArgs()...)
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.SysProcAttr = detachSysProcAttr()
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

// childArgs returns os.Args[1:] with the --bg flag removed.
func childArgs() []string {
	out := make([]string, 0, len(os.Args)-1)
	for _, a := range os.Args[1:] {
		if a == "--bg" || a == "-bg" {
			continue
		}
		out = append(out, a)
	}
	return out
}
