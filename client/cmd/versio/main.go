package main

import (
	"fmt"
	"io"
	"os"
)

// version is injected at build time: -ldflags "-X main.version=0.1.0".
var version = "dev"

const usage = "usage: versio [--version]\n"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	switch {
	// Run command without arguments
	case len(args) == 0:
		fmt.Fprintf(stdout, "Your version is %s\n", version)
		return 0
	// Run command with --version flag
	case len(args) == 1 && args[0] == "--version":
		fmt.Fprintf(stdout, "%s\n", version)
		return 0
	// Error in trying to fetch current version
	default:
		io.WriteString(stderr, usage)
		return 2
	}
}
