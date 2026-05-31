package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Runner runs external commands. Abstracted so tests can fake `tailscale`.
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

// funnelArgs builds the `tailscale funnel` argument list for a local proxy port.
func funnelArgs(proxyPort, publicPort int) []string {
	args := []string{"funnel", "--bg"}
	if publicPort != 443 {
		args = append(args, "--https", strconv.Itoa(publicPort))
	}
	return append(args, strconv.Itoa(proxyPort))
}

// funnelStart registers the Tailscale Funnel pointing at the local proxy port.
func funnelStart(r Runner, proxyPort, publicPort int) error {
	_, stderr, err := r.Run("tailscale", funnelArgs(proxyPort, publicPort)...)
	if err != nil {
		return fmt.Errorf("tailscale funnel failed: %v\n%s", err, stderr)
	}
	return nil
}

// funnelReset tears down the Funnel configuration.
func funnelReset(r Runner) error {
	_, stderr, err := r.Run("tailscale", "funnel", "reset")
	if err != nil {
		return fmt.Errorf("tailscale funnel reset failed: %v\n%s", err, stderr)
	}
	return nil
}

// funnelStatus returns the human-readable funnel status output.
func funnelStatus(r Runner) (string, error) {
	out, stderr, err := r.Run("tailscale", "funnel", "status")
	if err != nil {
		return "", fmt.Errorf("%v\n%s", err, stderr)
	}
	return out, nil
}

// nodeDNSName returns this node's MagicDNS name (without the trailing dot),
// e.g. "bigfoot.quoll-adhara.ts.net".
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

// publicBase returns the Funnel base URL, e.g. https://node.ts.net[:port].
func publicBase(node string, funnelPort int) string {
	if funnelPort == 443 {
		return "https://" + node
	}
	return fmt.Sprintf("https://%s:%d", node, funnelPort)
}
