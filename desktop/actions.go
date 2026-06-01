package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// copyToClipboard writes text to the system clipboard (pbcopy / clip / xclip|xsel).
func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("cmd", "/c", "clip")
	default:
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		}
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// killPID terminates a process (TERM on unix, taskkill /F on windows).
func killPID(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid")
	}
	if runtime.GOOS == "windows" {
		return exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/F").Run()
	}
	return exec.Command("kill", "-TERM", strconv.Itoa(pid)).Run()
}
