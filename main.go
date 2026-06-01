// Command tsp is the tailscale-proxy CLI. All logic lives in package core so the
// desktop app (and tests) can drive the same engine in-process.
package main

import (
	"os"

	"github.com/meabed/tailscale-proxy/core"
)

func main() { os.Exit(core.Run(os.Args[1:])) }
