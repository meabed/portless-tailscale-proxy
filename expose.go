package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Runner runs external commands. Abstracted so tests can fake `tailscale`/`lsof`.
type Runner interface {
	Run(name string, args ...string) (stdout, stderr string, err error)
}

type execRunner struct{}

func (execRunner) Run(name string, args ...string) (string, string, error) {
	cmd := exec.Command(name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.String(), errb.String(), err
}

// Mode selects the Tailscale exposure backend.
type Mode int

const (
	ModeFunnel Mode = iota // public internet
	ModeServe              // private, tailnet-only
)

func (m Mode) subcommand() string {
	if m == ModeServe {
		return "serve"
	}
	return "funnel"
}

func (m Mode) label() string {
	if m == ModeServe {
		return "Tailscale Serve (private)"
	}
	return "Tailscale Funnel (public)"
}

// exposeArgs builds the `tailscale serve|funnel` argument list.
func exposeArgs(mode Mode, proxyPort, publicPort int) []string {
	args := []string{mode.subcommand(), "--bg"}
	if publicPort != 443 {
		args = append(args, "--https", strconv.Itoa(publicPort))
	}
	return append(args, strconv.Itoa(proxyPort))
}

// exposeStart registers the Serve/Funnel entry for the local proxy port.
func exposeStart(r Runner, mode Mode, proxyPort, publicPort int) error {
	_, stderr, err := r.Run("tailscale", exposeArgs(mode, proxyPort, publicPort)...)
	if err != nil {
		return fmt.Errorf("tailscale %s failed: %v\n%s", mode.subcommand(), err, stderr)
	}
	return nil
}

// exposeReset removes the Serve/Funnel configuration.
func exposeReset(r Runner, mode Mode) error {
	_, stderr, err := r.Run("tailscale", mode.subcommand(), "reset")
	if err != nil {
		return fmt.Errorf("tailscale %s reset failed: %v\n%s", mode.subcommand(), err, stderr)
	}
	return nil
}

// exposeStatus returns the human-readable serve/funnel status.
func exposeStatus(r Runner, mode Mode) (string, error) {
	out, stderr, err := r.Run("tailscale", mode.subcommand(), "status")
	if err != nil {
		return "", fmt.Errorf("%v\n%s", err, stderr)
	}
	return out, nil
}

// nodeDNSName returns this node's MagicDNS name (without trailing dot).
func nodeDNSName(r Runner) (string, error) {
	out, stderr, err := r.Run("tailscale", "status", "--json")
	if err != nil {
		return "", fmt.Errorf("tailscale status failed: %v\n%s", err, stderr)
	}
	var s struct {
		Self struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		return "", err
	}
	name := strings.TrimSuffix(s.Self.DNSName, ".")
	if name == "" {
		return "", fmt.Errorf("this node has no MagicDNS name")
	}
	return name, nil
}

// publicBase returns the exposure base URL, e.g. https://node.ts.net[:port].
func publicBase(node string, publicPort int) string {
	if publicPort == 443 {
		return "https://" + node
	}
	return fmt.Sprintf("https://%s:%d", node, publicPort)
}
