package core

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

// acceptDNSEnabled reports whether this node accepts Tailscale DNS (MagicDNS).
// `tailscale debug prefs` exposes the pref as "CorpDNS". The second return is
// false when the state can't be determined (tailscale missing, parse failure).
func acceptDNSEnabled(r Runner) (on, known bool) {
	out, _, err := r.Run("tailscale", "debug", "prefs")
	if err != nil {
		return false, false
	}
	var p struct {
		CorpDNS bool `json:"CorpDNS"`
	}
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		return false, false
	}
	return p.CorpDNS, true
}

// setAcceptDNS runs `tailscale set --accept-dns=<val>` (val must be "true" or
// "false"). This is a global, persistent, machine-wide change — callers should
// only invoke it when the user explicitly opted in.
func setAcceptDNS(r Runner, val string) error {
	if val != "true" && val != "false" {
		return fmt.Errorf("accept-dns must be true or false, got %q", val)
	}
	_, stderr, err := r.Run("tailscale", "set", "--accept-dns="+val)
	if err != nil {
		return fmt.Errorf("tailscale set --accept-dns=%s failed: %v\n%s", val, err, stderr)
	}
	return nil
}

// publicBase returns the exposure base URL, e.g. https://node.ts.net[:port].
func publicBase(node string, publicPort int) string {
	if publicPort == 443 {
		return "https://" + node
	}
	return fmt.Sprintf("https://%s:%d", node, publicPort)
}
