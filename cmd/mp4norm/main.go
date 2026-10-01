// Command mp4norm normalizes MP4 container layout so files open instantly and
// seek quickly, without re-encoding.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/YLing2024/mp4norm/internal/ffmpeg"
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
	case "normalize":
		return runNormalize(args[1:])
	case "reencode":
		return runReencode(args[1:])
	case "batch":
		return runBatch(args[1:])
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
	return transform(in, outputPath(*out, in, ".norm.mp4"), normalize.Faststart)
}

func runNormalize(args []string) error {
	fs := flag.NewFlagSet("normalize", flag.ContinueOnError)
	out := fs.String("o", "", "output file (default: <input>.norm.mp4)")
	format := fs.String("format", "progressive", "output format: progressive or fmp4")
	window := fs.Int("window", 1000, "interleave window in milliseconds (progressive)")
	fragMs := fs.Int("frag-ms", 2000, "fragment duration in milliseconds (fmp4)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("normalize: missing <input>")
	}
	in := fs.Arg(0)
	outPath := outputPath(*out, in, ".norm.mp4")
	fn, err := normalizeFunc(*format, *window, *fragMs)
	if err != nil {
		return err
	}
	return transform(in, outPath, fn)
}

// normalizeFunc builds the lossless transform for the requested output format.
func normalizeFunc(format string, windowMs, fragMs int) (func(normalize.ReadSeekerAt, io.Writer) (*normalize.Result, error), error) {
	switch strings.ToLower(format) {
	case "progressive", "mp4", "prog":
		return func(src normalize.ReadSeekerAt, dst io.Writer) (*normalize.Result, error) {
			return normalize.Interleave(src, dst, normalize.InterleaveOptions{WindowMs: windowMs})
		}, nil
	case "fmp4", "fragmented":
		return func(src normalize.ReadSeekerAt, dst io.Writer) (*normalize.Result, error) {
			return normalize.Fragment(src, dst, normalize.FragmentOptions{FragmentMs: fragMs})
		}, nil
	default:
		return nil, fmt.Errorf("unknown format %q (want progressive or fmp4)", format)
	}
}

func runReencode(args []string) error {
	fs := flag.NewFlagSet("reencode", flag.ContinueOnError)
	out := fs.String("o", "", "output file (default: <input>.reenc.mp4)")
	codec := fs.String("vcodec", "h264", "video codec: h264, h265 or copy")
	hw := fs.String("hw", "auto", "hardware encoder: auto, on or off")
	crf := fs.Int("crf", 23, "quality, lower is better")
	preset := fs.String("preset", "medium", "encoder preset")
	audioBR := fs.String("audio-bitrate", "", "re-encode audio at this bitrate (default: copy)")
	gop := fs.Float64("gop", 2, "keyframe interval in seconds")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("reencode: missing <input>")
	}
	in := fs.Arg(0)
	outPath := outputPath(*out, in, ".reenc.mp4")

	opts := ffmpeg.Options{
		Video:        ffmpeg.VideoCodec(strings.ToLower(*codec)),
		Hardware:     ffmpeg.HardwareMode(strings.ToLower(*hw)),
		CRF:          *crf,
		Preset:       *preset,
		AudioBitrate: *audioBR,
		GOPSeconds:   *gop,
	}

	ctx := context.Background()
	cfg := ffmpeg.Config{}
	if ver, err := cfg.Version(ctx); err != nil {
		return err
	} else {
		fmt.Println(ver)
	}
	encoder, err := cfg.EncoderFor(ctx, opts)
	if err != nil {
		return err
	}
	fmt.Printf("encoder: %s (crf=%d preset=%s gop=%.1fs)\n", encoder, *crf, *preset, *gop)

	var last time.Time
	err = cfg.Run(ctx, in, outPath, opts, func(p ffmpeg.Progress) {
		if time.Since(last) < 200*time.Millisecond {
			return
		}
		last = time.Now()
		fmt.Printf("\rencoded %.1fs  frame %d  speed %s    ", float64(p.OutTime)/1e6, p.Frame, p.Speed)
	})
	fmt.Println()
	if err != nil {
		return err
	}

	fmt.Printf("wrote %s\n\n", outPath)
	rep, err := probe.Analyze(outPath)
	if err != nil {
		return err
	}
	fmt.Print(rep.String())
	return nil
}

// applyTransform runs a normalizer function against in, writing to outPath
// atomically via a temp file.
func applyTransform(in, outPath string, fn func(normalize.ReadSeekerAt, io.Writer) (*normalize.Result, error)) (*normalize.Result, error) {
	inFile, err := os.Open(in)
	if err != nil {
		return nil, err
	}
	defer inFile.Close()

	tmp, err := os.CreateTemp(filepath.Dir(outPath), ".mp4norm-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	res, err := fn(inFile, tmp)
	if err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpName, outPath); err != nil {
		return nil, err
	}
	return res, nil
}

// transform is applyTransform plus terminal output and a post-run probe.
func transform(in, outPath string, fn func(normalize.ReadSeekerAt, io.Writer) (*normalize.Result, error)) error {
	res, err := applyTransform(in, outPath, fn)
	if err != nil {
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

func runBatch(args []string) error {
	fs := flag.NewFlagSet("batch", flag.ContinueOnError)
	jobs := fs.Int("jobs", runtime.NumCPU(), "parallel workers")
	format := fs.String("format", "progressive", "output format: progressive or fmp4")
	window := fs.Int("window", 1000, "interleave window in milliseconds (progressive)")
	fragMs := fs.Int("frag-ms", 2000, "fragment duration in milliseconds (fmp4)")
	outdir := fs.String("outdir", "", "output directory (default: next to each input)")
	suffix := fs.String("suffix", ".norm.mp4", "output filename suffix")
	if err := fs.Parse(args); err != nil {
		return err
	}
	inputs, err := collectInputs(fs.Args())
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		return fmt.Errorf("batch: no input files")
	}
	fn, err := normalizeFunc(*format, *window, *fragMs)
	if err != nil {
		return err
	}
	if *outdir != "" {
		if err := os.MkdirAll(*outdir, 0o755); err != nil {
			return err
		}
	}

	if *jobs < 1 {
		*jobs = 1
	}
	sem := make(chan struct{}, *jobs)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var okCount, failCount int
	start := time.Now()

	for _, in := range inputs {
		in := in
		out := batchOutput(in, *outdir, *suffix)
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if _, err := applyTransform(in, out, fn); err != nil {
				mu.Lock()
				failCount++
				mu.Unlock()
				fmt.Fprintf(os.Stderr, "fail %s: %v\n", in, err)
				return
			}
			mu.Lock()
			okCount++
			mu.Unlock()
			fmt.Printf("ok   %s -> %s\n", in, out)
		}()
	}
	wg.Wait()

	fmt.Printf("\n%d ok, %d failed in %s\n", okCount, failCount, time.Since(start).Round(time.Millisecond))
	if failCount > 0 {
		return fmt.Errorf("batch: %d file(s) failed", failCount)
	}
	return nil
}

// collectInputs expands directories into their video files and keeps plain
// file arguments as-is.
func collectInputs(args []string) ([]string, error) {
	var inputs []string
	for _, a := range args {
		fi, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			inputs = append(inputs, a)
			continue
		}
		err = filepath.WalkDir(a, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".mp4", ".m4v", ".mov":
				inputs = append(inputs, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return inputs, nil
}

func batchOutput(in, outdir, suffix string) string {
	base := strings.TrimSuffix(filepath.Base(in), filepath.Ext(in)) + suffix
	if outdir == "" {
		return filepath.Join(filepath.Dir(in), base)
	}
	return filepath.Join(outdir, base)
}

func outputPath(out, in, suffix string) string {
	if out != "" {
		return out
	}
	base := strings.TrimSuffix(in, filepath.Ext(in))
	return base + suffix
}

func mib(n int64) float64 { return float64(n) / (1 << 20) }

func usage(w io.Writer) {
	fmt.Fprint(w, `mp4norm - normalize MP4 container layout for fast open and seek

Usage:
  mp4norm <command> [arguments]

Commands:
  probe <file>                       Inspect an MP4's container layout and report problems
  normalize [-format progressive|fmp4] [-window ms] [-frag-ms ms] [-o out] <input>
                                     Move moov to the front and interleave (progressive),
                                     or write a fragmented MP4 with sidx (fmp4). Lossless.
  faststart [-o out] <input>         Only move moov to the front (lossless, no re-encode)
  batch [-jobs n] [-format progressive|fmp4] [-outdir dir] [-suffix s] <input...|dir...>
                                     Normalize many files or directories in parallel
  reencode [-vcodec h264|h265|copy] [-hw auto|on|off] [-crf n] [-gop sec] [-o out] <input>
                                     Optional re-encode to fix sparse keyframes / VFR
  version                            Print the version
  help                               Show this help

`)
}
