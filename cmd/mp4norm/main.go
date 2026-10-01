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
	"github.com/YLing2024/mp4norm/internal/isobmff"
	"github.com/YLing2024/mp4norm/internal/normalize"
	"github.com/YLing2024/mp4norm/internal/probe"
)

// version is a var (not a const) so release builds can inject it with
// -ldflags "-X main.version=...".
var version = "0.1.0-dev"

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
		if len(args) > 1 {
			return fmt.Errorf("version: unexpected extra argument(s): %s", strings.Join(args[1:], " "))
		}
		fmt.Println("mp4norm", version)
		return nil
	case "probe":
		return runProbe(args[1:])
	case "faststart":
		return runFaststart(args[1:])
	case "normalize":
		return runNormalize(args[1:])
	case "reencode":
		return runReencode(args[1:])
	case "batch":
		return runBatch(args[1:])
	case "help", "-h", "--help":
		if len(args) > 1 {
			return fmt.Errorf("help: unexpected extra argument(s): %s", strings.Join(args[1:], " "))
		}
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runProbe(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("probe: missing <file>")
	}
	if len(args) > 1 {
		return fmt.Errorf("probe: unexpected extra argument(s): %s", strings.Join(args[1:], " "))
	}
	rep, err := probe.Analyze(args[0])
	if err != nil {
		return err
	}
	fmt.Print(rep.String())
	return nil
}

func runFaststart(args []string) error {
	fs := flag.NewFlagSet("faststart", flag.ContinueOnError)
	out := fs.String("o", "", "output file (default: <input>.norm.mp4)")
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	in, err := requireOneArg(fs, "faststart")
	if err != nil {
		return err
	}
	return transform(in, outputPath(*out, in, ".norm.mp4"), normalize.Faststart, false)
}

func runNormalize(args []string) error {
	fs := flag.NewFlagSet("normalize", flag.ContinueOnError)
	out := fs.String("o", "", "output file (default: <input>.norm.mp4)")
	format := fs.String("format", "progressive", "output format: progressive or fmp4")
	window := fs.Int("window", 1000, "interleave window in milliseconds (progressive)")
	fragMs := fs.Int("frag-ms", 2000, "fragment duration in milliseconds (fmp4)")
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	in, err := requireOneArg(fs, "normalize")
	if err != nil {
		return err
	}
	outPath := outputPath(*out, in, ".norm.mp4")
	fn, fragmented, err := normalizeFunc(*format, *window, *fragMs)
	if err != nil {
		return err
	}
	return transform(in, outPath, fn, fragmented)
}

// normalizeFunc builds the lossless transform for the requested output format
// and reports whether it produces a fragmented MP4.
func normalizeFunc(format string, windowMs, fragMs int) (func(normalize.ReadSeekerAt, io.Writer) (*normalize.Result, error), bool, error) {
	switch strings.ToLower(format) {
	case "progressive", "mp4", "prog":
		return func(src normalize.ReadSeekerAt, dst io.Writer) (*normalize.Result, error) {
			return normalize.Interleave(src, dst, normalize.InterleaveOptions{WindowMs: windowMs})
		}, false, nil
	case "fmp4", "fragmented":
		return func(src normalize.ReadSeekerAt, dst io.Writer) (*normalize.Result, error) {
			return normalize.Fragment(src, dst, normalize.FragmentOptions{FragmentMs: fragMs})
		}, true, nil
	default:
		return nil, false, fmt.Errorf("unknown format %q (want progressive or fmp4)", format)
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
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	in, err := requireOneArg(fs, "reencode")
	if err != nil {
		return err
	}
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
	if err != nil {
		return err
	}
	fmt.Println()

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

	src, err := validExtent(inFile)
	if err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(outPath), ".mp4norm-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	res, err := fn(src, tmp)
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

// validExtent returns a read view of f limited to its well-formed top-level
// boxes. Trailing bytes that do not form a box (a malformed final box or a
// short residue) are hidden so that strict MP4 decoders — mp4ff in particular —
// accept files that our own scanner already treats as valid. The lossless
// transforms never copy those bytes, so the output is unaffected.
func validExtent(f *os.File) (normalize.ReadSeekerAt, error) {
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	boxes, trunc, err := isobmff.ScanTopLevel(f, fi.Size())
	if err != nil {
		return nil, err
	}
	end := fi.Size()
	if trunc != nil {
		// Last box claims more than exists; keep only the boxes before it.
		end = trunc.Offset
	} else if len(boxes) > 0 {
		end = boxes[len(boxes)-1].End()
	}
	if end >= fi.Size() || end <= 0 {
		return f, nil
	}
	return io.NewSectionReader(f, 0, end), nil
}

// transform is applyTransform plus terminal output and a post-run probe.
// fragmented selects the fMP4 wording for the summary line.
func transform(in, outPath string, fn func(normalize.ReadSeekerAt, io.Writer) (*normalize.Result, error), fragmented bool) error {
	res, err := applyTransform(in, outPath, fn)
	if err != nil {
		return err
	}

	rep, err := probe.Analyze(outPath)
	if err != nil {
		return err
	}

	fmt.Printf("wrote %s\n", outPath)
	if fragmented {
		fmt.Printf("  size %s -> %s, fragmented: %d moof/mdat pairs\n",
			humanSize(res.InputSize), humanSize(res.OutputSize), rep.MdatCount)
	} else {
		fmt.Printf("  size %s -> %s, moov moved to front (mdat shifted %+d B)\n",
			humanSize(res.InputSize), humanSize(res.OutputSize), res.Delta)
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
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	inputs, err := collectInputs(fs.Args())
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		return fmt.Errorf("batch: no input files")
	}
	fn, _, err := normalizeFunc(*format, *window, *fragMs)
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

// humanSize formats a byte count with a magnitude-appropriate unit and two
// decimals, so small files don't all collapse to "0.00 MiB".
func humanSize(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%.2f B", float64(n))
	case n < 1<<20:
		return fmt.Sprintf("%.2f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%.2f MiB", float64(n)/(1<<20))
	}
}

// reorderArgs moves flag-like tokens (and their separate values) ahead of
// positional arguments, so the standard library flag parser accepts flags and
// positionals in any order. `--` stops flag processing; tokens after it are
// kept as positionals.
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		// A separate value must travel with its flag. Inline values (-o=x) and
		// boolean flags don't consume the next token.
		if name := strings.TrimLeft(a, "-"); strings.Contains(name, "=") {
			continue
		} else if f := fs.Lookup(name); f != nil && !isBoolFlag(f) && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...)
}

// requireOneArg returns the single positional argument, or an error naming any
// extra ones. It is used by the commands that operate on exactly one input.
func requireOneArg(fs *flag.FlagSet, cmd string) (string, error) {
	rest := fs.Args()
	if len(rest) < 1 {
		return "", fmt.Errorf("%s: missing <input>", cmd)
	}
	if len(rest) > 1 {
		return "", fmt.Errorf("%s: unexpected extra argument(s): %s", cmd, strings.Join(rest[1:], " "))
	}
	return rest[0], nil
}

func isBoolFlag(f *flag.Flag) bool {
	type boolFlag interface{ IsBoolFlag() bool }
	bf, ok := f.Value.(boolFlag)
	return ok && bf.IsBoolFlag()
}

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
