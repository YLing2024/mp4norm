// Command mp4norm normalizes MP4 container layout so files open instantly and
// seek quickly, without re-encoding.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/YLing2024/mp4norm/internal/normalize"
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
	case "faststart":
		return runFaststart(args[1:])
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

func runFaststart(args []string) error {
	fs := flag.NewFlagSet("faststart", flag.ContinueOnError)
	out := fs.String("o", "", "output file (default: <input>.norm.mp4)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("faststart: missing <input>")
	}
	in := fs.Arg(0)

	outPath := *out
	if outPath == "" {
		outPath = defaultOutput(in, ".norm.mp4")
	}

	inFile, err := os.Open(in)
	if err != nil {
		return err
	}
	defer inFile.Close()

	tmp, err := os.CreateTemp(filepath.Dir(outPath), ".mp4norm-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	res, err := normalize.Faststart(inFile, tmp)
	if err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, outPath); err != nil {
		return err
	}

	fmt.Printf("wrote %s\n", outPath)
	fmt.Printf("  size %.1f MiB -> %.1f MiB, mdat moved %+d bytes, changed=%t\n",
		mib(res.InputSize), mib(res.OutputSize), res.Delta, res.Changed)

	rep, err := probe.Analyze(outPath)
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Print(rep.String())
	return nil
}

func defaultOutput(in, suffix string) string {
	ext := filepath.Ext(in)
	base := strings.TrimSuffix(in, ext)
	if ext == "" {
		ext = ".mp4"
	}
	return base + suffix
}

func mib(n int64) float64 { return float64(n) / (1 << 20) }

func usage(w io.Writer) {
	fmt.Fprint(w, `mp4norm - normalize MP4 container layout for fast open and seek

Usage:
  mp4norm <command> [arguments]

Commands:
  probe <file>                     Inspect an MP4's container layout and report problems
  faststart [-o out] <input>       Move moov to the front (lossless, no re-encode)
  version                          Print the version
  help                             Show this help

`)
}
