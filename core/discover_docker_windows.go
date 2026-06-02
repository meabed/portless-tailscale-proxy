//go:build windows

package core

// dockerListeners is a no-op on Windows — Docker API over Unix socket
// is not available. Returns nil so the caller treats it as "no docker listeners".
func (d *Discoverer) dockerListeners(rng PortRange) []listener {
	return nil
}

// mergeDockerListeners is a no-op on Windows.
func (d *Discoverer) mergeDockerListeners(lsofListeners []listener, rng PortRange) []listener {
	return lsofListeners
}

// dockerAvailable always returns false on Windows since we rely on Unix sockets.
func dockerAvailable() bool {
	return false
}
