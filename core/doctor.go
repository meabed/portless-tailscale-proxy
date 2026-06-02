package core

import (
	"fmt"
	"strings"
)

// Check is one preflight result with a remediation hint. Fix prints only when
// the check fails; Note is advisory and prints whether or not the check passed.
type Check struct {
	Name   string
	OK     bool
	Detail string
	Fix    string
	Note   string
}

const (
	linkTailscaleInstall = "https://tailscale.com/download"
	linkFunnelKB         = "https://tailscale.com/kb/1223/funnel"
	linkHTTPSKB          = "https://tailscale.com/kb/1153/enabling-https"
)

// runDoctor probes tailscale, exposure readiness, lsof, and discovery.
func runDoctor(r Runner, disc *Discoverer, cfg discoverConfig, mode Mode) []Check {
	var checks []Check

	// Docker check (only when --docker is used).
	if cfg.docker {
		checks = append(checks, dockerCheck())
	}

	verOut, _, verErr := r.Run("tailscale", "version")
	if verErr != nil {
		checks = append(checks, Check{
			Name: "tailscale installed", OK: false,
			Detail: "`tailscale` not found on PATH",
			Fix:    "Install Tailscale: " + linkTailscaleInstall,
		})
	} else {
		checks = append(checks, Check{Name: "tailscale installed", OK: true, Detail: firstLine(verOut)})

		statusOut, _, statusErr := r.Run("tailscale", "status")
		if statusErr != nil || strings.Contains(statusOut, "Logged out") {
			checks = append(checks, Check{
				Name: "tailscale up", OK: false, Detail: "node is not logged in",
				Fix: "Run: tailscale up   (https://tailscale.com/kb/1080/cli#up)",
			})
		} else {
			checks = append(checks, Check{Name: "tailscale up", OK: true})
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
				checks = append(checks, Check{Name: "funnel enabled", OK: true})
			}

			// Advisory: on a tailnet node with MagicDNS (accept-dns) on, *.ts.net
			// resolves to the tailnet IP, so the public Funnel URL won't reach the
			// public ingress when opened from that node. Informational only — not a
			// failure, and not something tsp changes for you (it's a global, machine-
			// wide setting that belongs on the consuming host).
			if on, known := acceptDNSEnabled(r); known && on {
				checks = append(checks, Check{
					Name: "magicdns", OK: true, Detail: "accept-dns on",
					Note: "MagicDNS resolves *.ts.net to tailnet IPs, so the public funnel URL\n" +
						"won't reach the public ingress when opened from a tailnet node.\n" +
						"To consume the public funnel from such a host, run there:\n" +
						"  tailscale set --accept-dns=false   (re-enable with =true)",
				})
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
		checks = append(checks, Check{Name: "service discovery", OK: ok, Detail: detail, Fix: fix})
	}

	return checks
}

// dockerCheck returns a Check for Docker socket availability.
func dockerCheck() Check {
	if dockerAvailable() {
		return Check{Name: "docker available", OK: true, Detail: "socket accessible"}
	}
	return Check{
		Name:   "docker available",
		OK:     false,
		Detail: "Docker socket not accessible at /var/run/docker.sock",
		Fix:    "Start Docker or ensure the socket is available",
	}
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
		if c.Note != "" {
			nl := strings.Split(c.Note, "\n")
			fmt.Println("    note: " + nl[0])
			for _, l := range nl[1:] {
				fmt.Println("          " + l)
			}
		}
	}
	return allOK
}
