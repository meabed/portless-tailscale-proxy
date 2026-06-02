//go:build !windows

package core

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const dockerSocketPath = "/var/run/docker.sock"

type dockerPortBinding struct {
	PublicPort  int    `json:"PublicPort"`
	PrivatePort int    `json:"PrivatePort"`
	IP          string `json:"IP"`
}

type dockerNetworkInfo struct {
	IPAddress string `json:"IPAddress"`
}

type dockerContainerInfo struct {
	Names           []string            `json:"Names"`
	Ports           []dockerPortBinding `json:"Ports"`
	NetworkSettings struct {
		Networks map[string]dockerNetworkInfo `json:"Networks"`
	} `json:"NetworkSettings"`
}

// dockerListeners queries the Docker API for running containers and returns
// listeners for each port found. Returns nil (not an error) if Docker is
// unavailable — the caller (discover_unix.go) treats this as "no docker
// listeners" rather than a failure.
func (d *Discoverer) dockerListeners(rng PortRange) []listener {
	if _, err := os.Stat(dockerSocketPath); err != nil {
		return nil
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", dockerSocketPath)
			},
		},
	}

	resp, err := client.Get("http://localhost/containers/json")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	return parseDockerListeners(body, rng)
}

func parseDockerListeners(body []byte, rng PortRange) []listener {
	var containers []dockerContainerInfo
	if err := json.Unmarshal(body, &containers); err != nil {
		return nil
	}

	var result []listener
	for ci, c := range containers {
		if len(c.Names) == 0 || len(c.Ports) == 0 {
			continue
		}
		name := strings.TrimPrefix(c.Names[0], "/")
		containerIP := firstContainerIP(c.NetworkSettings.Networks)

		for pi, p := range c.Ports {
			port := p.PublicPort
			host := "127.0.0.1"
			if port == 0 {
				if containerIP == "" {
					continue
				}
				port = p.PrivatePort
				host = containerIP
			}
			if !rng.contains(port) {
				continue
			}
			result = append(result, listener{
				Port: port,
				Host: host,
				PID:  syntheticDockerPID(ci, pi),
				Comm: "docker",
				Cwd:  name,
			})
		}
	}

	return result
}

func firstContainerIP(networks map[string]dockerNetworkInfo) string {
	for _, n := range networks {
		if n.IPAddress != "" {
			return n.IPAddress
		}
	}
	return ""
}

func syntheticDockerPID(containerIndex, portIndex int) int {
	return -1 - containerIndex*1000 - portIndex
}

// dockerAvailable checks if the Docker socket is accessible and the API responds.
func dockerAvailable() bool {
	if _, err := os.Stat(dockerSocketPath); err != nil {
		return false
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", dockerSocketPath)
			},
		},
	}

	resp, err := client.Get("http://localhost/version")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
