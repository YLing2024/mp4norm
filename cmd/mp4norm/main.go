// Command mp4norm normalizes MP4 container layout so files open instantly and
// seek quickly, without re-encoding.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/YLing2024/mp4norm/internal/classify"
	"github.com/YLing2024/mp4norm/internal/ffmpeg"
	"github.com/YLing2024/mp4norm/internal/isobmff"
	"github.com/YLing2024/mp4norm/internal/normalize"
	"github.com/YLing2024/mp4norm/internal/probe"
)

// version is a var (not a const) so release builds can inject it with
// -ldflags "-X main.version=...".
var version = "0.1.0-dev"

func main() {
	err := run(os.Args[1:])
	if err == nil {
		return
	}
	// `scan` reports its outcome through the exit code without it being an
	// error to print (0/1/2 = clean / needs work / broken).
	var ec exitCodeError
	if errors.As(err, &ec) {
		os.Exit(ec.code)
	}
	fmt.Fprintln(os.Stderr, "mp4norm: error:", err)
	os.Exit(1)
}

// exitCodeError carries a process exit status that is not itself a failure to
// report. main honours it without printing an error line.
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

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
	case "scan":
		return runScan(args[1:])
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

// runScan classifies every file under the given directories (or the files
// themselves) and prints a plain-language table. Exit code 0 = all clean,
// 1 = something needs work, 2 = something is broken or could not be read.
func runScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	verbose := fs.Bool("v", false, "show the raw probe fields under each file")
	needsWorkOnly := fs.Bool("needs-work", false, "only list files that need work")
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	inputs := fs.Args()
	if len(inputs) == 0 {
		return fmt.Errorf("scan: missing <directory|file...>")
	}

	files, bad := classify.Collect(inputs)
	verdicts := classify.All(files)
	for _, f := range bad {
		verdicts = append(verdicts, classify.BrokenFromError(f.Path, f.Err))
	}
	sort.SliceStable(verdicts, func(i, j int) bool {
		if verdicts[i].Name != verdicts[j].Name {
			return verdicts[i].Name < verdicts[j].Name
		}
		return verdicts[i].Path < verdicts[j].Path
	})

	ok, nw, broken := scanSummary(verdicts)
	target := scanTarget(inputs)

	if *asJSON {
		shown := verdicts
		if *needsWorkOnly {
			shown = filterNeedsWork(verdicts)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(scanJSON{
			Target: target,
			Counts: scanCountsJSON{OK: ok, NeedsWork: nw, Broken: broken, Total: len(verdicts)},
			Files:  shown,
		}); err != nil {
			return err
		}
	} else {
		printScanReport(os.Stdout, target, verdicts, *needsWorkOnly, *verbose)
	}

	if code := scanExitCode(ok, nw, broken); code != 0 {
		return exitCodeError{code: code}
	}
	return nil
}

// scanSummary tallies verdicts by status.
func scanSummary(vs []classify.Verdict) (ok, needsWork, broken int) {
	for _, v := range vs {
		switch v.Status {
		case classify.StatusNeedsWork:
			needsWork++
		case classify.StatusBroken:
			broken++
		default:
			ok++
		}
	}
	return ok, needsWork, broken
}

func scanExitCode(ok, needsWork, broken int) int {
	if broken > 0 {
		return 2
	}
	if needsWork > 0 {
		return 1
	}
	return 0
}

func filterNeedsWork(vs []classify.Verdict) []classify.Verdict {
	out := make([]classify.Verdict, 0, len(vs))
	for _, v := range vs {
		if v.Status == classify.StatusNeedsWork {
			out = append(out, v)
		}
	}
	return out
}

func scanTarget(inputs []string) string {
	if len(inputs) == 1 {
		return inputs[0]
	}
	return fmt.Sprintf("%d 个位置", len(inputs))
}

type scanCountsJSON struct {
	OK        int `json:"ok"`
	NeedsWork int `json:"needsWork"`
	Broken    int `json:"broken"`
	Total     int `json:"total"`
}

type scanJSON struct {
	Target string             `json:"target"`
	Counts scanCountsJSON     `json:"counts"`
	Files  []classify.Verdict `json:"files"`
}

// printScanReport writes the human table grouped by verdict, most urgent
// first. Raw fields only appear with verbose.
func printScanReport(w io.Writer, target string, vs []classify.Verdict, needsWorkOnly, verbose bool) {
	ok, needsWork, broken := scanSummary(vs)
	fmt.Fprintf(w, "扫描 %s —— 发现 %d 个 MP4 文件\n\n", target, len(vs))

	var needs, brokens, oks []classify.Verdict
	for _, v := range vs {
		switch v.Status {
		case classify.StatusNeedsWork:
			needs = append(needs, v)
		case classify.StatusBroken:
			brokens = append(brokens, v)
		default:
			oks = append(oks, v)
		}
	}

	shown := append([]classify.Verdict{}, needs...)
	if !needsWorkOnly {
		shown = append(shown, brokens...)
		shown = append(shown, oks...)
	}
	nameW, sizeW := scanWidths(shown)

	if len(needs) > 0 {
		fmt.Fprintf(w, "需要处理（%d）：\n", len(needs))
		printScanRows(w, needs, nameW, sizeW, verbose)
		fmt.Fprintln(w)
	}
	if !needsWorkOnly {
		if len(brokens) > 0 {
			fmt.Fprintf(w, "无法处理（%d）：\n", len(brokens))
			printScanRows(w, brokens, nameW, sizeW, verbose)
			fmt.Fprintln(w)
		}
		if len(oks) > 0 {
			fmt.Fprintf(w, "无需处理（%d）：\n", len(oks))
			printScanRows(w, oks, nameW, sizeW, verbose)
			fmt.Fprintln(w)
		}
	}
	fmt.Fprintf(w, "共 %d 个：%d 个建议规整 / %d 个无需处理 / %d 个无法处理\n",
		len(vs), needsWork, ok, broken)
	if needsWork > 0 {
		fmt.Fprintln(w, "下一步：mp4norm batch -outdir <输出目录> <目录>      批量规整")
	}
}

func printScanRows(w io.Writer, vs []classify.Verdict, nameW, sizeW int, verbose bool) {
	for _, v := range vs {
		fmt.Fprintf(w, "  %s %s  %s  %s\n",
			scanIcon(v.Status), padRight(v.Name, nameW), padLeft(probe.HumanBytes(v.Size), sizeW),
			v.ReasonsText("zh"))
		if !verbose {
			continue
		}
		if v.Probe != nil {
			fmt.Fprintf(w, "      brand=%s  moov=%s  mdat=%d  fragmented=%t  size=%d B\n",
				orDash(v.Probe.MajorBrand), v.Probe.MoovPosition, v.Probe.MdatCount,
				v.Probe.Fragmented, v.Size)
		} else {
			fmt.Fprintf(w, "      size=%d B  error=%s\n", v.Size, v.Error)
		}
	}
}

func scanIcon(s classify.Status) string {
	switch s {
	case classify.StatusNeedsWork:
		return "⚠️"
	case classify.StatusBroken:
		return "❌"
	default:
		return "✅"
	}
}

func orDash(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

func scanWidths(vs []classify.Verdict) (nameW, sizeW int) {
	for _, v := range vs {
		if w := dispWidth(v.Name); w > nameW {
			nameW = w
		}
		if w := dispWidth(probe.HumanBytes(v.Size)); w > sizeW {
			sizeW = w
		}
	}
	return nameW, sizeW
}

// dispWidth approximates the number of terminal cells a string occupies, so
// names containing CJK characters line up in the scan table.
func dispWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func runeWidth(r rune) int {
	switch {
	case r >= 0xFE00 && r <= 0xFE0F, r >= 0x200B && r <= 0x200F:
		return 0 // variation selectors and zero-width marks
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE4F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x20000 && r <= 0x3FFFD:
		return 2
	}
	return 1
}

func padRight(s string, w int) string {
	if n := w - dispWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func padLeft(s string, w int) string {
	if n := w - dispWidth(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
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
  scan [-v] [-needs-work] [-json] <dir|file...>
                                     Scan a folder and say, in plain language, which files
                                     are fine, which are worth normalizing, and which are
                                     broken. Exit code 0/1/2 = clean/needs work/broken.
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
