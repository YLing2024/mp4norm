// Command mp4norm normalizes MP4 container layout so files open instantly and
// seek quickly, without re-encoding.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/YLing2024/mp4norm/internal/probe"
)

const version = "0.1.0-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mp4norm: error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stdout)
		return nil
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Println("mp4norm", version)
		return nil
	case "probe":
		if len(args) < 2 {
			return fmt.Errorf("probe: missing <file>")
		}
		return runProbe(args[1])
	case "help", "-h", "--help":
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runProbe(path string) error {
	rep, err := probe.Analyze(path)
	if err != nil {
		return err
	}
	fmt.Print(rep.String())
	return nil
}

func usage(w io.Writer) {
	fmt.Fprint(w, `mp4norm - normalize MP4 container layout for fast open and seek

Usage:
  mp4norm <command> [arguments]

Commands:
  probe <file>   Inspect an MP4's container layout and report problems
  version        Print the version
  help           Show this help

`)
}
