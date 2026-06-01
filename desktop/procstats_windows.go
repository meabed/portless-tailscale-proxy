//go:build windows

package main

type procStat struct {
	CPU    float64
	MemMB  int
	Uptime string
}

// procStats is a no-op on Windows for now (no cheap batched ps equivalent).
func procStats(pids []int) map[int]procStat { return map[int]procStat{} }
