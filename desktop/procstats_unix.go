//go:build !windows

package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type procStat struct {
	CPU    float64 // percent
	MemMB  int     // resident set, MB
	Uptime string  // compact process age, e.g. "2h13m"
}

// procStats batches one `ps` call for the given pids (macOS/Linux).
func procStats(pids []int) map[int]procStat {
	out := map[int]procStat{}
	if len(pids) == 0 {
		return out
	}
	list := make([]string, len(pids))
	for i, p := range pids {
		list[i] = strconv.Itoa(p)
	}
	// pid, %cpu, rss(KB), elapsed-time — values only, no header.
	cmd := exec.Command("ps", "-o", "pid=,%cpu=,rss=,etime=", "-p", strings.Join(list, ","))
	b, err := cmd.Output()
	if err != nil {
		return out
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) < 4 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		cpu, _ := strconv.ParseFloat(f[1], 64)
		rss, _ := strconv.Atoi(f[2])
		out[pid] = procStat{CPU: cpu, MemMB: rss / 1024, Uptime: humanEtime(f[3])}
	}
	return out
}

// humanEtime turns ps etime ([[dd-]hh:]mm:ss) into "HH:MM" (or "Nd HH:MM").
func humanEtime(s string) string {
	days := 0
	if i := strings.IndexByte(s, '-'); i >= 0 {
		days, _ = strconv.Atoi(s[:i])
		s = s[i+1:]
	}
	parts := strings.Split(s, ":")
	var h, m int
	switch len(parts) {
	case 3:
		h, _ = strconv.Atoi(parts[0])
		m, _ = strconv.Atoi(parts[1])
	case 2:
		m, _ = strconv.Atoi(parts[0])
	default:
		return s
	}
	hm := fmt.Sprintf("%02d:%02d", h, m)
	if days > 0 {
		return fmt.Sprintf("%dd %s", days, hm)
	}
	return hm
}
