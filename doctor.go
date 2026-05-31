package main

import (
	"fmt"
	"os"
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
	linkPortless         = "https://portless.sh"
)

// runDoctor probes tailscale, Funnel, and portless and returns ordered checks.
func runDoctor(r Runner, statePath string) []Check {
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

	if _, err := os.Stat(statePath); err != nil {
		checks = append(checks, Check{
			Name: "portless routes", OK: false, Detail: statePath + " not found",
			Fix: "Install & start portless:\n" +
				"  - " + linkPortless + "\n" +
				"  - npm install -g portless\n" +
				"  - portless proxy start",
		})
	} else if m, err := loadRoutes(statePath); err != nil {
		checks = append(checks, Check{"portless routes", false, "parse error: " + err.Error(), "Inspect " + statePath})
	} else {
		checks = append(checks, Check{"portless routes", true, fmt.Sprintf("%d route(s)", len(m)), ""})
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
