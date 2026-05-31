package main

import (
	"fmt"
	"os"
	"strings"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches the subcommand and returns a process exit code. With no
// subcommand (or a leading flag), it runs `start` with the saved config.
func run(argv []string) int {
	if len(argv) == 0 {
		return cmdStart(nil)
	}
	switch argv[0] {
	case "start":
		return cmdStart(argv[1:])
	case "list":
		return cmdList(argv[1:])
	case "status":
		return cmdStatus(argv[1:])
	case "reset":
		return cmdReset(argv[1:])
	case "doctor":
		return cmdDoctor(argv[1:])
	case "configure", "config":
		return cmdConfigure(argv[1:])
	case "update":
		return cmdUpdate(argv[1:])
	case "-v", "--version", "version":
		fmt.Println(version)
		return 0
	case "-h", "--help", "help":
		printHelp()
		return 0
	default:
		// A leading flag means "start with these flags" (start is the default).
		if strings.HasPrefix(argv[0], "-") {
			return cmdStart(argv)
		}
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", argv[0])
		printHelp()
		return 1
	}
}
