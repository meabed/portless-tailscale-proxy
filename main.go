package main

import (
	"fmt"
	"os"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches the subcommand and returns a process exit code.
func run(argv []string) int {
	if len(argv) == 0 {
		printHelp()
		return 1
	}
	switch argv[0] {
	case "start":
		return cmdStart(argv[1:])
	case "reset":
		return cmdReset(argv[1:])
	case "status":
		return cmdStatus(argv[1:])
	case "list":
		return cmdList(argv[1:])
	case "doctor":
		return cmdDoctor(argv[1:])
	case "-v", "--version", "version":
		fmt.Println(version)
		return 0
	case "-h", "--help", "help":
		printHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", argv[0])
		printHelp()
		return 1
	}
}
