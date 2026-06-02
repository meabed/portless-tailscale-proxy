//go:build !windows

package core

import (
	"fmt"
	"strconv"
	"strings"
)

// listeners lists listening TCP sockets in range via lsof, enriching each with
// the full runtime (ps) and working directory (lsof -d cwd).
func (d *Discoverer) listeners(rng PortRange) ([]listener, error) {
	out, stderr, err := d.run.Run("lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpcn")
	if err != nil {
		// Distinguish "lsof not installed" from "lsof found nothing" (lsof exits
		// non-zero with empty output when no sockets match — not an error for us).
		if strings.Contains(err.Error(), "executable file not found") ||
			strings.Contains(stderr, "not found") {
			return nil, fmt.Errorf("lsof not found — install it (macOS ships it; Linux: apt/dnf install lsof)")
		}
		if strings.TrimSpace(out) == "" {
			return []listener{}, nil
		}
		// Non-zero exit but we still got output — parse what we have.
	}
	ls := parseLsofListeners(out, rng)
	if len(ls) == 0 {
		return ls, nil
	}

	pids := uniquePIDs(ls)
	if psOut, _, err := d.run.Run("ps", "-o", "pid=,comm=", "-p", strings.Join(pids, ",")); err == nil {
		comm := parsePsComm(psOut)
		for i := range ls {
			if c, ok := comm[ls[i].PID]; ok {
				ls[i].PsComm = c // keep the lsof Comm; ps is a secondary signal
			}
		}
	}

	cwdArgs := []string{"-a", "-d", "cwd", "-Fpn", "-p", strings.Join(pids, ",")}
	if cwdOut, _, err := d.run.Run("lsof", cwdArgs...); err == nil {
		cwd := parseLsofCwd(cwdOut)
		for i := range ls {
			if c, ok := cwd[ls[i].PID]; ok {
				ls[i].Cwd = c
			}
		}
	}
	return ls, nil
}

// mergeDockerListeners appends Docker-discovered listeners to lsof results,
// deduplicating by port (lsof takes priority).
func (d *Discoverer) mergeDockerListeners(lsofListeners []listener, rng PortRange) []listener {
	dockerLs := d.dockerListeners(rng)
	if len(dockerLs) == 0 {
		return lsofListeners
	}

	// Build a set of ports already covered by lsof.
	covered := make(map[int]bool, len(lsofListeners))
	for _, l := range lsofListeners {
		covered[l.Port] = true
	}

	for _, dl := range dockerLs {
		if !covered[dl.Port] {
			lsofListeners = append(lsofListeners, dl)
			covered[dl.Port] = true
		}
	}

	return lsofListeners
}

// parseLsofListeners parses `lsof -Fpcn` output, deduping per (pid,port).
func parseLsofListeners(out string, rng PortRange) []listener {
	var res []listener
	seen := map[string]bool{}
	var pid int
	var comm string
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
			comm = ""
		case 'c':
			comm = line[1:]
		case 'n':
			port := portFromAddr(line[1:])
			if port == 0 || !rng.contains(port) {
				continue
			}
			key := strconv.Itoa(pid) + ":" + strconv.Itoa(port)
			if seen[key] {
				continue
			}
			seen[key] = true
			res = append(res, listener{Port: port, PID: pid, Comm: comm})
		}
	}
	return res
}

// portFromAddr extracts the port from an lsof name like "*:4764" or "[::1]:3000".
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

// parsePsComm parses `ps -o pid=,comm=` output into pid -> executable path.
func parsePsComm(out string) map[int]string {
	m := map[int]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		m[pid] = strings.Join(f[1:], " ")
	}
	return m
}

// parseLsofCwd parses `lsof -d cwd -Fpn` output into pid -> cwd.
func parseLsofCwd(out string) map[int]string {
	m := map[int]string{}
	var pid int
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'n':
			if pid != 0 {
				m[pid] = line[1:]
			}
		}
	}
	return m
}

// uniquePIDs returns the distinct PIDs of a listener slice as strings.
func uniquePIDs(ls []listener) []string {
	seen := map[int]bool{}
	var out []string
	for _, l := range ls {
		if !seen[l.PID] {
			seen[l.PID] = true
			out = append(out, strconv.Itoa(l.PID))
		}
	}
	return out
}
