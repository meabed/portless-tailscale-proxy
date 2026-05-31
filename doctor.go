package main

import (
	"fmt"
	"strings"
)

// Check is one preflight result with a remediation hint.
type Check struct {
	Name   string
	OK     bool
	Detail string
	Fix    string
}

const (
	linkTailscaleInstall = "https://tailscale.com/download"
	linkFunnelKB         = "https://tailscale.com/kb/1223/funnel"
	linkHTTPSKB          = "https://tailscale.com/kb/1153/enabling-https"
)

// runDoctor probes tailscale, exposure readiness, lsof, and discovery.
func runDoctor(r Runner, disc *Discoverer, cfg discoverConfig, mode Mode) []Check {
	var checks []Check

	verOut, _, verErr := r.Run("tailscale", "version")
	if verErr != nil {
		checks = append(checks, Check{
			Name: "tailscale installed", OK: false,
			Detail: "`tailscale` not found on PATH",
			Fix:    "Install Tailscale: " + linkTailscaleInstall,
		})
	} else {
		checks = append(checks, Check{"tailscale installed", true, firstLine(verOut), ""})

		statusOut, _, statusErr := r.Run("tailscale", "status")
		if statusErr != nil || strings.Contains(statusOut, "Logged out") {
			checks = append(checks, Check{
				Name: "tailscale up", OK: false, Detail: "node is not logged in",
				Fix: "Run: tailscale up   (https://tailscale.com/kb/1080/cli#up)",
			})
		} else {
			checks = append(checks, Check{"tailscale up", true, "", ""})
		}

		if mode == ModeFunnel {
			_, fStderr, fErr := r.Run("tailscale", "funnel", "status")
			if fErr != nil {
				checks = append(checks, Check{
					Name: "funnel enabled", OK: false, Detail: strings.TrimSpace(fStderr),
					Fix: "Enable Funnel for your tailnet:\n" +
						"  - Overview: " + linkFunnelKB + "\n" +
						"  - Enable HTTPS certs: " + linkHTTPSKB + "\n" +
						"  - Grant the `funnel` node attribute in your tailnet policy file (admin console)",
				})
			} else {
				checks = append(checks, Check{"funnel enabled", true, "", ""})
			}
		}
	}

	// Discovery readiness.
	svcs, _, derr := disc.Discover(cfg)
	if derr != nil {
		checks = append(checks, Check{
			Name: "service discovery", OK: false, Detail: derr.Error(),
			Fix: "Ensure `lsof` is installed (macOS has it; Linux: `apt install lsof` / `dnf install lsof`)",
		})
	} else {
		detail := fmt.Sprintf("%d service(s) in %d-%d", len(svcs), cfg.rng.Lo, cfg.rng.Hi)
		fix := ""
		ok := true
		if len(svcs) == 0 {
			ok = false
			detail = fmt.Sprintf("no services found in %d-%d", cfg.rng.Lo, cfg.rng.Hi)
			fix = "Start a dev server in range, widen --ports, or pass --all to include non-web processes"
		}
		checks = append(checks, Check{"service discovery", ok, detail, fix})
	}

	return checks
}

// firstLine returns the first line of s, trimmed.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// printChecks writes a ✓/✗ summary and returns true if every check passed.
func printChecks(checks []Check) bool {
	allOK := true
	for _, c := range checks {
		mark := "✓"
		if !c.OK {
			mark = "✗"
			allOK = false
		}
		line := fmt.Sprintf("%s %s", mark, c.Name)
		if c.Detail != "" {
			line += "  (" + c.Detail + ")"
		}
		fmt.Println(line)
		if !c.OK && c.Fix != "" {
			for _, fl := range strings.Split(c.Fix, "\n") {
				fmt.Println("    " + fl)
			}
		}
	}
	return allOK
}
