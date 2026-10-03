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
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
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
	fmt.Fprintln(os.Stderr, "mp4norm: 错误:", humanizeError(err))
	os.Exit(1)
}

// exitCodeError carries a process exit status that is not itself a failure to
// report. main honours it without printing an error line.
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// humanizeError rewrites an error chain so no raw operating-system text (for
// example "GetFileAttributesEx H:\...: The system cannot find the file
// specified.") ever reaches the user. Our own errors are already Chinese; this
// only replaces the low-level leaves and splices the translation back into the
// surrounding context, so a prefix like "back up original: " is preserved.
func humanizeError(err error) error {
	if err == nil {
		return nil
	}
	if text, ok := osErrorText(err); ok {
		return errors.New(text)
	}
	if cause := errors.Unwrap(err); cause != nil {
		translated := humanizeError(cause)
		if translated.Error() == cause.Error() {
			return err
		}
		return errors.New(strings.Replace(err.Error(), cause.Error(), translated.Error(), 1))
	}
	return err
}

// osErrorText recognises the filesystem errors the standard library returns and
// renders them in plain Chinese. It matches only the error value itself (not a
// wrapped cause) so callers keep their own context.
func osErrorText(err error) (string, bool) {
	switch e := err.(type) {
	case *fs.PathError:
		return pathErrorText(e.Path, e.Err), true
	case *os.LinkError:
		return pathErrorText(e.Old, e.Err), true
	case *os.SyscallError:
		if text, ok := syscallErrorText(e.Err); ok {
			return text, true
		}
		return "系统调用失败", true
	case syscall.Errno:
		if text, ok := syscallErrorText(e); ok {
			return text, true
		}
		return "系统调用失败", true
	}
	return "", false
}

// pathErrorText names the file and the reason, never the raw Windows call.
func pathErrorText(path string, cause error) string {
	text, ok := syscallErrorText(cause)
	if !ok {
		text = "无法访问"
	}
	if path == "" {
		return text
	}
	return text + "：" + path
}

// syscallErrorText maps the filesystem conditions a user can act on to plain
// Chinese. Anything else is reported generically rather than leaking the OS
// wording.
func syscallErrorText(err error) (string, bool) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "找不到文件", true
	case errors.Is(err, fs.ErrPermission):
		return "没有权限读取", true
	case errors.Is(err, fs.ErrExist):
		return "目标已存在", true
	case errors.Is(err, syscall.ENOSPC):
		return "磁盘空间不足", true
	}
	return "", false
}

func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stdout)
		return nil
	}
	switch args[0] {
	case "version", "--version", "-v":
		if len(args) > 1 {
			return fmt.Errorf("version: 多了不需要的参数：%s", strings.Join(args[1:], " "))
		}
		fmt.Println("mp4norm", version)
		return nil
	case "probe":
		return runProbe(args[1:])
	case "check", "scan":
		// `scan` is a hidden alias of `check`, kept for old docs and scripts.
		return runScan(args[1:])
	case "faststart":
		return runFaststart(args[1:])
	case "fix", "normalize", "batch":
		// `normalize` and `batch` are hidden aliases kept so old docs and
		// scripts keep working; `fix` is the documented command.
		return runFix(args[1:])
	case "reencode":
		return runReencode(args[1:])
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
			return fmt.Errorf("help: 多了不需要的参数：%s", strings.Join(args[1:], " "))
		}
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return fmt.Errorf("未知命令 %q", args[0])
	}
}

func runProbe(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("probe: 请给出要检查的文件")
	}
	if len(args) > 1 {
		return fmt.Errorf("probe: 多了不需要的参数：%s", strings.Join(args[1:], " "))
	}
	rep, err := probe.Analyze(args[0])
	if err != nil {
		return err
	}
	fmt.Print(rep.String())
	return nil
}

// runScan is the engine behind `check` (and its hidden `scan` alias). It
// classifies every file under the given directories, or the files themselves,
// and prints a plain-language table. Exit code 0 = all clean, 1 = something
// needs work, 2 = something is broken or could not be read.
func runScan(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	verbose := fs.Bool("v", false, "在每个文件下面显示原始探测字段")
	needsWorkOnly := fs.Bool("needs-work", false, "只列出需要处理的文件")
	asJSON := fs.Bool("json", false, "机器可读的 JSON 输出")
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	inputs := fs.Args()
	if len(inputs) == 0 {
		return fmt.Errorf("check: 请给出要检查的文件或目录")
	}

	files, bad := classify.Collect(inputs)
	verdicts := classify.All(files)
	for _, f := range bad {
		verdicts = append(verdicts, classify.BrokenFromError(f.Path, humanizeError(f.Err)))
	}
	sort.SliceStable(verdicts, func(i, j int) bool {
		if verdicts[i].Name != verdicts[j].Name {
			return verdicts[i].Name < verdicts[j].Name
		}
		return verdicts[i].Path < verdicts[j].Path
	})

	ok, nw, broken := scanSummary(verdicts)
	target := scanTarget(inputs)
	nextStep := scanNextStep(inputs)

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
		printScanReport(os.Stdout, target, verdicts, *needsWorkOnly, *verbose, nextStep)
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

// scanNextStep suggests the follow-up `fix` invocation in the shape that
// matches the input: a single named file gets the plain single-file form, while
// a directory (or several inputs) gets the -outdir form.
func scanNextStep(inputs []string) string {
	if len(inputs) == 1 {
		if fi, err := os.Stat(inputs[0]); err == nil && !fi.IsDir() {
			return "下一步：mp4norm fix <文件>"
		}
	}
	return "下一步：mp4norm fix -outdir <输出目录> <目录>      规整这些文件"
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
func printScanReport(w io.Writer, target string, vs []classify.Verdict, needsWorkOnly, verbose bool, nextStep string) {
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
	if needsWork > 0 && nextStep != "" {
		fmt.Fprintln(w, nextStep)
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
	out := fs.String("o", "", "输出文件（默认：<输入>.norm.mp4）")
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

// runFix is the unified lossless entry point. It accepts any number of files
// and/or directories and applies the same container rewrite to every input, so
// there is no separate "single file" and "batch" mode. A single explicitly
// named file takes the one-shot path (the only one that honours -o); anything
// else is expanded and run through the concurrent engine. The legacy
// `normalize` and `batch` commands are hidden aliases of this one.
func runFix(args []string) error {
	fs := flag.NewFlagSet("fix", flag.ContinueOnError)
	out := fs.String("o", "", "输出文件（仅限单个文件；默认：<输入>.norm.mp4）")
	format := fs.String("format", "progressive", "输出格式：progressive 或 fmp4")
	window := fs.Int("window", 1000, "交织窗口，单位毫秒（progressive）")
	fragMs := fs.Int("frag-ms", 2000, "分片时长，单位毫秒（fmp4）")
	jobs := fs.Int("jobs", runtime.NumCPU(), "并行任务数")
	outdir := fs.String("outdir", "", "输出目录（默认：每个输入文件旁边）")
	suffix := fs.String("suffix", ".norm.mp4", "输出文件名后缀")
	inPlace, backupDir := outputModeFlags(fs)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	raw := fs.Args()
	if len(raw) == 0 {
		return fmt.Errorf("fix: 请给出要处理的文件或目录")
	}
	if *inPlace && (*outdir != "" || *out != "") {
		return fmt.Errorf("fix: -in-place 与 -outdir（以及 -o）不能同时使用")
	}
	if !*inPlace && *backupDir != "" {
		return fmt.Errorf("fix: -backup-dir 需要配合 -in-place 使用")
	}
	fn, fragmented, err := normalizeFunc(*format, *window, *fragMs)
	if err != nil {
		return err
	}

	// One explicitly named file with default placement keeps the one-shot
	// path; a directory, a mix, or -outdir goes through the concurrent engine.
	if len(raw) == 1 && *outdir == "" {
		if fi, statErr := os.Stat(raw[0]); statErr == nil && !fi.IsDir() {
			spec, err := buildSpec("fix", raw[0], *out, *suffix, *inPlace, *backupDir)
			if err != nil {
				return err
			}
			if err := runTransform(raw[0], spec, fn, fragmented); err != nil {
				return reencodeAdvice(err)
			}
			return nil
		}
	}

	inputs, err := collectInputs(raw)
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		return fmt.Errorf("fix: 没有找到可处理的文件")
	}
	if *out != "" {
		return fmt.Errorf("fix: -o 只能用于单个输入文件")
	}
	return runConcurrent(inputs, batchSettings{
		cmd:       "fix",
		jobs:      *jobs,
		outdir:    *outdir,
		suffix:    *suffix,
		inPlace:   *inPlace,
		backupDir: *backupDir,
	}, fn, fragmented)
}

// outputModeFlags registers the in-place output mode shared by the lossless
// commands. Both flags are opt-in, so their absence keeps the historical
// "write a new file" behavior exactly.
func outputModeFlags(fs *flag.FlagSet) (*bool, *string) {
	inPlace := fs.Bool("in-place", false, "备份后替换原文件")
	backupDir := fs.String("backup-dir", "", "就地备份的存放目录（默认：<输入所在目录>/.mp4norm-backup）")
	return inPlace, backupDir
}

// transformSpec is the resolved output mode for one lossless rewrite.
type transformSpec struct {
	inPlace   bool
	backupDir string
	output    string
}

// transformFunc is a lossless container rewrite: read from src, write to dst.
type transformFunc func(normalize.ReadSeekerAt, io.Writer) (*normalize.Result, error)

// buildSpec validates the output-mode flags and resolves the new-file path.
func buildSpec(cmd, in, out, suffix string, inPlace bool, backupDir string) (transformSpec, error) {
	if inPlace && out != "" {
		return transformSpec{}, fmt.Errorf("%s: -in-place 与 -o 不能同时使用", cmd)
	}
	if !inPlace && backupDir != "" {
		return transformSpec{}, fmt.Errorf("%s: -backup-dir 需要配合 -in-place 使用", cmd)
	}
	spec := transformSpec{inPlace: inPlace, backupDir: backupDir}
	if !inPlace {
		spec.output = outputPath(out, in, suffix)
	}
	return spec, nil
}

// runTransform performs one lossless rewrite through the shared safe write path
// and prints the before/after benefit summary.
func runTransform(in string, spec transformSpec, fn transformFunc, fragmented bool) error {
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
func normalizeFunc(format string, windowMs, fragMs int) (transformFunc, bool, error) {
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
		return nil, false, fmt.Errorf("未知格式 %q（可选 progressive 或 fmp4）", format)
	}
}

func runReencode(args []string) error {
	fs := flag.NewFlagSet("reencode", flag.ContinueOnError)
	out := fs.String("o", "", "输出文件（默认：<输入>.reenc.mp4）")
	codec := fs.String("vcodec", "h264", "视频编码：h264、h265 或 copy")
	hw := fs.String("hw", "auto", "硬件编码器：auto、on 或 off")
	crf := fs.Int("crf", 23, "画质，数值越低越好")
	preset := fs.String("preset", "medium", "编码预设")
	audioBR := fs.String("audio-bitrate", "", "以此码率重编码音频（默认：copy）")
	gop := fs.Float64("gop", 2, "关键帧间隔，单位秒")
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

// reencodeAdvice wraps a lossless-fix failure with a pointer to the separate
// `reencode` command. Some files (very sparse keyframes, VFR) cannot be fixed
// by container surgery alone and need a real re-encode, which is deliberately
// not folded into `fix`.
func reencodeAdvice(err error) error {
	return fmt.Errorf("%w\n  如果这个文件无法靠无损方式修好（例如关键帧过于稀疏或 VFR），可以试试：mp4norm reencode <文件>", err)
}

// batchSettings carries the resolved options for a multi-file lossless rewrite.
type batchSettings struct {
	cmd       string // command name used in error messages
	jobs      int
	outdir    string
	suffix    string
	inPlace   bool
	backupDir string
}

// runConcurrent is the shared engine that rewrites many files in parallel. It
// backs the legacy `batch` command and the directory/multi-input path of `fix`,
// so both share one implementation of the concurrent safe-write loop.
func runConcurrent(inputs []string, s batchSettings, fn transformFunc, fragmented bool) error {
	if s.inPlace && s.outdir != "" {
		return fmt.Errorf("%s: -in-place 与 -outdir 不能同时使用", s.cmd)
	}
	if !s.inPlace && s.backupDir != "" {
		return fmt.Errorf("%s: -backup-dir 需要配合 -in-place 使用", s.cmd)
	}
	if s.outdir != "" {
		if err := os.MkdirAll(s.outdir, 0o755); err != nil {
			return err
		}
	}
	if s.jobs < 1 {
		s.jobs = 1
	}

	type item struct {
		in  string
		out string
		oc  *safefile.Outcome
		err error
	}
	results := make([]item, len(inputs))
	sem := make(chan struct{}, s.jobs)
	var wg sync.WaitGroup
	start := time.Now()

	for i, in := range inputs {
		i, in := i, in
		out := batchOutput(in, s.outdir, s.suffix)
		if s.inPlace {
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
				InPlace:    s.inPlace,
				BackupDir:  s.backupDir,
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
			fmt.Fprintf(os.Stderr, "fail %s: %v\n", r.in, reencodeAdvice(r.err))
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
		return fmt.Errorf("%s: %d 个文件处理失败", s.cmd, failCount)
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
		return fmt.Errorf("backups: 请给出一个备份目录（只能一个）")
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
	dirFlag := fs.String("backup-dir", "", "备份目录（默认：文件旁边）")
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
	dirFlag := fs.String("backup-dir", "", "备份目录（默认：文件旁边）")
	yes := fs.Bool("yes", false, "确认删除")
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
		return fmt.Errorf("tmp: 请给出要检查的目录")
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
			return nil, humanizeError(err)
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
			return nil, humanizeError(err)
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
		return "", fmt.Errorf("%s: 请给出输入文件", cmd)
	}
	if len(rest) > 1 {
		return "", fmt.Errorf("%s: 多了不需要的参数：%s", cmd, strings.Join(rest[1:], " "))
	}
	return rest[0], nil
}

func isBoolFlag(f *flag.Flag) bool {
	type boolFlag interface{ IsBoolFlag() bool }
	bf, ok := f.Value.(boolFlag)
	return ok && bf.IsBoolFlag()
}

func usage(w io.Writer) {
	fmt.Fprint(w, `mp4norm - 规整 MP4 容器的排布，让文件快速打开、拖动顺畅

用法：
  mp4norm <命令> [参数]

命令：
  check [-v] [-needs-work] [-json] <文件|目录...>
                                     检查文件和目录，用人话说清哪些文件正常、
                                     哪些值得规整、哪些已经损坏。
                                     退出码 0/1/2 = 无需处理/建议规整/无法处理。
  fix [-format progressive|fmp4] [-window ms] [-frag-ms ms] [-jobs n]
      [-outdir dir] [-suffix s] [-in-place] [-backup-dir dir] <文件|目录...>
                                     修复（无损重写）任意数量的文件和/或目录：
                                     单个文件与整个目录走同一条流程。-in-place
                                     会在备份后替换原文件，且与 -outdir（以及
                                     -o）互斥；-backup-dir 需要配合 -in-place 使用。

高级命令：
  probe <file>                       检查 MP4 的容器排布并报告问题
  faststart [-o out] [-in-place] [-backup-dir dir] <input>
                                     只把 moov 移到文件开头（无损，不重新编码）
  reencode [-vcodec h264|h265|copy] [-hw auto|on|off] [-crf n] [-gop sec] [-o out] <input>
                                     可选的重编码，用于修复关键帧稀疏 / VFR
  backups <directory>                列出某个目录下保存的备份
  restore [-backup-dir dir] <file>   把文件最新的一份备份放回原位
  forget [-backup-dir dir] --yes <file>
                                     删除某个文件的备份（不加 --yes 时只列出）
  tmp <directory...>                 列出中断残留的临时文件
  watch [-interval 5m] [-outdir dir] [-in-place] [-backup-dir dir] [-once] [-quiet] <dir...>
                                     轮询目录，等新文件停止写入后再规整。第一轮
                                     只做基线观察：记录文件后放着不动，从第二轮
                                     起才开始处理。仍在变化的文件记为 writing，
                                     绝不记为失败。-in-place（带备份）与 -outdir
                                     互斥；-once 只跑一轮观察并以 scan 风格的
                                     退出码（0/1/2）退出——需要跑两次才能
                                     「先观察、再处理」。
  version                            打印版本号
  help                               显示本帮助

`)
}
