//go:build windows

package core

import (
	"fmt"
	"strconv"
	"strings"
)

// listeners lists listening TCP sockets via netstat, with process names from
// tasklist. Working directory is not available on Windows.
func (d *Discoverer) listeners(rng PortRange) ([]listener, error) {
	out, stderr, err := d.run.Run("netstat", "-ano", "-p", "TCP")
	if err != nil {
		return nil, fmt.Errorf("netstat failed: %v\n%s", err, stderr)
	}
	ls := parseNetstat(out, rng)
	if len(ls) == 0 {
		return ls, nil
	}
	if tlOut, _, err := d.run.Run("tasklist", "/FO", "CSV", "/NH"); err == nil {
		names := parseTasklist(tlOut)
		for i := range ls {
			if n, ok := names[ls[i].PID]; ok {
				ls[i].Comm = n
			}
		}
	}
	return ls, nil
}

// parseNetstat parses `netstat -ano -p TCP`, keeping LISTENING rows in range.
func parseNetstat(out string, rng PortRange) []listener {
	var res []listener
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "TCP" || f[3] != "LISTENING" {
			continue
		}
		port := portFromAddr(f[1])
		pid, err := strconv.Atoi(f[4])
		if port == 0 || err != nil || !rng.contains(port) {
			continue
		}
		key := f[4] + ":" + strconv.Itoa(port)
		if seen[key] {
			continue
		}
		seen[key] = true
		res = append(res, listener{Port: port, PID: pid})
	}
	return res
}

// portFromAddr extracts the port from "0.0.0.0:3000" or "[::]:4983".
func portFromAddr(addr string) int {
	i := strings.LastIndexByte(addr, ':')
	if i < 0 {
		return 0
	}
	p, err := strconv.Atoi(addr[i+1:])
	if err != nil {
		return 0
	}
	return p
}

// parseTasklist parses `tasklist /FO CSV /NH` into pid -> image name.
func parseTasklist(out string) map[int]string {
	m := map[int]string{}
	for _, line := range strings.Split(out, "\n") {
		cols := splitCSV(line)
		if len(cols) < 2 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(cols[1]))
		if err != nil {
			continue
		}
		m[pid] = strings.TrimSpace(cols[0])
	}
	return m
}

// splitCSV splits a simple quoted CSV line (no embedded quotes).
func splitCSV(line string) []string {
	var cols []string
	for _, c := range strings.Split(line, ",") {
		cols = append(cols, strings.Trim(strings.TrimSpace(c), "\""))
	}
	return cols
}
