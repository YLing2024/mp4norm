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

	"github.com/YLing2024/mp4norm/internal/backup"
	"github.com/YLing2024/mp4norm/internal/classify"
	"github.com/YLing2024/mp4norm/internal/ffmpeg"
	"github.com/YLing2024/mp4norm/internal/normalize"
	"github.com/YLing2024/mp4norm/internal/probe"
	"github.com/YLing2024/mp4norm/internal/safefile"
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
	case "backups":
		return runBackups(args[1:])
	case "restore":
		return runRestore(args[1:])
	case "forget":
		return runForget(args[1:])
	case "tmp":
		return runTmp(args[1:])
	case "watch":
		return runWatch(args[1:])
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
	inPlace, backupDir := outputModeFlags(fs)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	in, err := requireOneArg(fs, "faststart")
	if err != nil {
		return err
	}
	spec, err := buildSpec("faststart", in, *out, ".norm.mp4", *inPlace, *backupDir)
	if err != nil {
		return err
	}
	return runTransform(in, spec, normalize.Faststart, false)
}

func runNormalize(args []string) error {
	fs := flag.NewFlagSet("normalize", flag.ContinueOnError)
	out := fs.String("o", "", "output file (default: <input>.norm.mp4)")
	format := fs.String("format", "progressive", "output format: progressive or fmp4")
	window := fs.Int("window", 1000, "interleave window in milliseconds (progressive)")
	fragMs := fs.Int("frag-ms", 2000, "fragment duration in milliseconds (fmp4)")
	inPlace, backupDir := outputModeFlags(fs)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	in, err := requireOneArg(fs, "normalize")
	if err != nil {
		return err
	}
	spec, err := buildSpec("normalize", in, *out, ".norm.mp4", *inPlace, *backupDir)
	if err != nil {
		return err
	}
	fn, fragmented, err := normalizeFunc(*format, *window, *fragMs)
	if err != nil {
		return err
	}
	return runTransform(in, spec, fn, fragmented)
}

// outputModeFlags registers the in-place output mode shared by the lossless
// commands. Both flags are opt-in, so their absence keeps the historical
// "write a new file" behavior exactly.
func outputModeFlags(fs *flag.FlagSet) (*bool, *string) {
	inPlace := fs.Bool("in-place", false, "replace the input file after backing it up")
	backupDir := fs.String("backup-dir", "", "directory for in-place backups (default: <input dir>/.mp4norm-backup)")
	return inPlace, backupDir
}

// transformSpec is the resolved output mode for one lossless rewrite.
type transformSpec struct {
	inPlace   bool
	backupDir string
	output    string
}

// buildSpec validates the output-mode flags and resolves the new-file path.
func buildSpec(cmd, in, out, suffix string, inPlace bool, backupDir string) (transformSpec, error) {
	if inPlace && out != "" {
		return transformSpec{}, fmt.Errorf("%s: -in-place and -o are mutually exclusive", cmd)
	}
	if !inPlace && backupDir != "" {
		return transformSpec{}, fmt.Errorf("%s: -backup-dir requires -in-place", cmd)
	}
	spec := transformSpec{inPlace: inPlace, backupDir: backupDir}
	if !inPlace {
		spec.output = outputPath(out, in, suffix)
	}
	return spec, nil
}

// runTransform performs one lossless rewrite through the shared safe write path
// and prints the before/after benefit summary.
func runTransform(in string, spec transformSpec, fn func(normalize.ReadSeekerAt, io.Writer) (*normalize.Result, error), fragmented bool) error {
	oc, err := safefile.Transform(safefile.Request{
		Input:      in,
		Output:     spec.output,
		InPlace:    spec.inPlace,
		BackupDir:  spec.backupDir,
		Fragmented: fragmented,
		Transform:  fn,
		Logf:       cleanupLog,
	})
	if err != nil {
		return err
	}
	printOutcome(oc, fragmented)
	return nil
}

// cleanupLog reports best-effort cleanup notes (stale temp residue removed
// before a rewrite) on stderr, leaving the command's stdout report untouched.
func cleanupLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "mp4norm: "+format+"\n", args...)
}

// printOutcome writes the "wrote ..." line followed by the before/after proof.
func printOutcome(oc *safefile.Outcome, fragmented bool) {
	fmt.Printf("wrote %s\n", oc.Output)
	if fragmented {
		fmt.Printf("  size %s -> %s, fragmented: %d moof/mdat pairs\n",
			humanSize(oc.Result.InputSize), humanSize(oc.Result.OutputSize), reportMdat(oc.After))
	} else {
		fmt.Printf("  size %s -> %s, moov moved to front (mdat shifted %+d B)\n",
			humanSize(oc.Result.InputSize), humanSize(oc.Result.OutputSize), oc.Result.Delta)
	}
	if oc.BackupPath != "" {
		fmt.Printf("  original backed up to %s\n", oc.BackupPath)
	}
	fmt.Println()
	printBenefit(os.Stdout, oc.Before, oc.After, fragmented)
	// Keep the long-standing diagnostic dump so default-mode output is not
	// impoverished by the added summary.
	if oc.After != nil {
		fmt.Println()
		fmt.Print(oc.After.String())
	}
}

// reportMdat guards against a nil post-probe report.
func reportMdat(r *probe.Report) int {
	if r == nil {
		return 0
	}
	return r.MdatCount
}

// printBenefit renders the before/after container improvements. Sizes are
// clearly marked as approximations: they describe what a player must read
// before the first frame, not a guarantee.
func printBenefit(w io.Writer, before, after *probe.Report, fragmented bool) {
	if before == nil || after == nil {
		return
	}
	fmt.Fprintln(w, "✅ 已完成（无损，画质不变）")
	if !fragmented {
		fmt.Fprintf(w, "   首次可播放需读取：约 %s → 约 %s\n",
			probe.HumanBytes(before.FirstPlayBytes), probe.HumanBytes(after.FirstPlayBytes))
		fmt.Fprintf(w, "   数据碎片：%d 块 → %d 块\n", before.MdatCount, after.MdatCount)
	} else {
		fmt.Fprintf(w, "   首次可播放需读取：约 %s → 约 %s\n",
			probe.HumanBytes(before.FirstPlayBytes), probe.HumanBytes(after.FirstPlayBytes))
		fmt.Fprintf(w, "   分片：%d 段\n", after.MdatCount)
	}
	fmt.Fprintf(w, "   体积：%s → %s\n", probe.HumanBytes(before.FileSize), probe.HumanBytes(after.FileSize))
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

func runBatch(args []string) error {
	fs := flag.NewFlagSet("batch", flag.ContinueOnError)
	jobs := fs.Int("jobs", runtime.NumCPU(), "parallel workers")
	format := fs.String("format", "progressive", "output format: progressive or fmp4")
	window := fs.Int("window", 1000, "interleave window in milliseconds (progressive)")
	fragMs := fs.Int("frag-ms", 2000, "fragment duration in milliseconds (fmp4)")
	outdir := fs.String("outdir", "", "output directory (default: next to each input)")
	suffix := fs.String("suffix", ".norm.mp4", "output filename suffix")
	inPlace, backupDir := outputModeFlags(fs)
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
	if *inPlace && *outdir != "" {
		return fmt.Errorf("batch: -in-place and -outdir are mutually exclusive")
	}
	if !*inPlace && *backupDir != "" {
		return fmt.Errorf("batch: -backup-dir requires -in-place")
	}
	fn, fragmented, err := normalizeFunc(*format, *window, *fragMs)
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
	type item struct {
		in  string
		out string
		oc  *safefile.Outcome
		err error
	}
	results := make([]item, len(inputs))
	sem := make(chan struct{}, *jobs)
	var wg sync.WaitGroup
	start := time.Now()

	for i, in := range inputs {
		i, in := i, in
		out := batchOutput(in, *outdir, *suffix)
		if *inPlace {
			out = in
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			oc, err := safefile.Transform(safefile.Request{
				Input:      in,
				Output:     out,
				InPlace:    *inPlace,
				BackupDir:  *backupDir,
				Fragmented: fragmented,
				Transform:  fn,
				Logf:       cleanupLog,
			})
			results[i] = item{in: in, out: out, oc: oc, err: err}
		}()
	}
	wg.Wait()

	var okCount, failCount int
	var beforeFP, afterFP int64
	for _, r := range results {
		if r.err != nil {
			failCount++
			fmt.Fprintf(os.Stderr, "fail %s: %v\n", r.in, r.err)
			continue
		}
		okCount++
		fmt.Printf("ok   %s -> %s\n", r.in, r.out)
		printBenefit(os.Stdout, r.oc.Before, r.oc.After, fragmented)
		if r.oc.Before != nil && r.oc.After != nil {
			beforeFP += r.oc.Before.FirstPlayBytes
			afterFP += r.oc.After.FirstPlayBytes
		}
	}

	fmt.Printf("\n%d ok, %d failed in %s\n", okCount, failCount, time.Since(start).Round(time.Millisecond))
	if okCount > 0 {
		fmt.Printf("共 %d 个文件，合计“首次可播放需读取”从约 %s 降到约 %s\n",
			okCount, probe.HumanBytes(beforeFP), probe.HumanBytes(afterFP))
	}
	if failCount > 0 {
		return fmt.Errorf("batch: %d file(s) failed", failCount)
	}
	return nil
}

// runBackups lists the backups stored for a directory (or a backup directory
// itself).
func runBackups(args []string) error {
	fs := flag.NewFlagSet("backups", flag.ContinueOnError)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("backups: want exactly one <directory>")
	}
	dir := backup.ResolveDir(rest[0])
	entries, err := backup.List(dir)
	if err != nil {
		return err
	}
	fmt.Printf("备份目录：%s\n", dir)
	if len(entries) == 0 {
		fmt.Println("没有找到备份。")
		return nil
	}
	fmt.Printf("找到 %d 份备份（新到的在前）：\n\n", len(entries))
	for _, e := range entries {
		state := "原文件缺失"
		if e.OriginalExists {
			state = "原文件存在"
		}
		missing := ""
		if !e.BackupExists {
			missing = " [备份文件缺失]"
		}
		fmt.Printf("  %s\n", e.Original)
		fmt.Printf("    备份=%s%s  大小=%s  时间=%s  %s\n",
			e.Backup, missing, probe.HumanBytes(e.Size),
			e.Created.Local().Format("2006-01-02 15:04:05"), state)
	}
	return nil
}

// runRestore puts the newest backup of a file back in place. If the file still
// exists, its current version is backed up first so the restore is undoable.
func runRestore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	dirFlag := fs.String("backup-dir", "", "backup directory (default: alongside the file)")
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	file, err := requireOneArg(fs, "restore")
	if err != nil {
		return err
	}
	dir := *dirFlag
	if dir == "" {
		dir = backup.DefaultDir(file)
	}
	res, err := backup.Restore(dir, file)
	if err != nil {
		return err
	}
	fmt.Printf("已恢复 %s\n", file)
	fmt.Printf("  使用备份  %s（%s，%s）\n", res.Restored.Backup,
		probe.HumanBytes(res.Restored.Size),
		res.Restored.Created.Local().Format("2006-01-02 15:04:05"))
	if res.Replaced != nil {
		fmt.Printf("  被覆盖的当前版本已另存为备份  %s\n", res.Replaced.Backup)
	}
	return nil
}

// runForget deletes every backup of a file. Without --yes it only lists them.
func runForget(args []string) error {
	fs := flag.NewFlagSet("forget", flag.ContinueOnError)
	dirFlag := fs.String("backup-dir", "", "backup directory (default: alongside the file)")
	yes := fs.Bool("yes", false, "confirm deletion")
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	file, err := requireOneArg(fs, "forget")
	if err != nil {
		return err
	}
	dir := *dirFlag
	if dir == "" {
		dir = backup.DefaultDir(file)
	}
	entries, err := backup.Find(dir, file)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Printf("没有找到 %s 的备份。\n", file)
		return nil
	}
	if !*yes {
		fmt.Printf("将删除 %s 的 %d 份备份（加 --yes 才会执行）：\n", file, len(entries))
		for _, e := range entries {
			fmt.Printf("  %s\n", e.Backup)
		}
		return nil
	}
	n, err := backup.Forget(dir, file)
	if err != nil {
		return err
	}
	fmt.Printf("已删除 %s 的 %d 份备份。\n", file, n)
	return nil
}

// runTmp lists the rewrite temp files found in the given directories. It is
// read-only: the next rewrite sweeps stale files automatically, so this exists
// purely to show a user what an interrupted run left behind and how big it is.
func runTmp(args []string) error {
	fs := flag.NewFlagSet("tmp", flag.ContinueOnError)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	dirs := fs.Args()
	if len(dirs) == 0 {
		return fmt.Errorf("tmp: missing <directory...>")
	}
	now := time.Now()
	found := 0
	fmt.Println("残留临时文件：")
	for _, dir := range dirs {
		for _, t := range safefile.ListTempFiles(dir) {
			state := "较新"
			if now.Sub(t.ModTime) >= safefile.StaleTempAfter {
				state = "陈旧"
			}
			fmt.Printf("  [%s] %s  %s  %s\n",
				state, probe.HumanBytes(t.Size), now.Sub(t.ModTime).Round(time.Minute), t.Path)
			found++
		}
	}
	if found == 0 {
		fmt.Println("  没有发现。")
		return nil
	}
	fmt.Printf("\n共 %d 个（陈旧的会在下次规整时自动清理）。\n", found)
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
  normalize [-format progressive|fmp4] [-window ms] [-frag-ms ms] [-o out] [-in-place] [-backup-dir dir] <input>
                                     Move moov to the front and interleave (progressive),
                                     or write a fragmented MP4 with sidx (fmp4). Lossless.
                                     -in-place replaces the input (a backup is kept); it is
                                     mutually exclusive with -o.
  faststart [-o out] [-in-place] [-backup-dir dir] <input>
                                     Only move moov to the front (lossless, no re-encode)
  batch [-jobs n] [-format progressive|fmp4] [-outdir dir] [-suffix s] [-in-place] [-backup-dir dir] <input...|dir...>
                                     Normalize many files or directories in parallel
  backups <directory>                List the backups stored for a directory
  restore [-backup-dir dir] <file>   Put a file's newest backup back in place
  forget [-backup-dir dir] --yes <file>
                                     Delete a file's backups (without --yes, just list them)
  tmp <directory...>                 List leftover temp files from an interrupted run
  watch [-interval 5m] [-outdir dir] [-in-place] [-backup-dir dir] [-once] [-quiet] <dir...>
                                     Poll folders and normalize new files once they stop
                                     being written. -in-place (with a backup) and -outdir
                                     are mutually exclusive; -once runs one round and exits
                                     with scan-style codes (0/1/2).
  reencode [-vcodec h264|h265|copy] [-hw auto|on|off] [-crf n] [-gop sec] [-o out] <input>
                                     Optional re-encode to fix sparse keyframes / VFR
  version                            Print the version
  help                               Show this help

`)
}
