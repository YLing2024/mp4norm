package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/YLing2024/mp4norm/internal/classify"
	"github.com/YLing2024/mp4norm/internal/ffmpeg"
	"github.com/YLing2024/mp4norm/internal/normalize"
	"github.com/YLing2024/mp4norm/internal/probe"
)

// App is the Wails application, exposing the mp4norm kernel to the frontend.
type App struct {
	ctx  context.Context
	lang string

	// initialTargets holds the file/directory paths the user passed on the
	// command line (e.g. `mp4norm "D:\videos"`). The frontend reads them via
	// InitialTargets on startup and scans them through the normal batch path.
	initialTargets []string

	// Batch state. batchCancel is non-nil while a normalize-all run is in
	// progress so CancelBatch can stop it.
	batchMu     sync.Mutex
	batchCancel context.CancelFunc
}

// NewApp creates a new App. Any initialTargets are the file/directory paths
// parsed from the command line; they are reported to the frontend but do not
// trigger any work on their own.
func NewApp(initialTargets ...string) *App {
	return &App{lang: "zh", initialTargets: initialTargets}
}

// wailsValueFlags names the Wails runtime flags that consume a following
// argument (see wailsapp/wails internal/app). Command-line parsing skips
// these so `wails dev -loglevel debug` never mistakes "debug" for a path.
var wailsValueFlags = map[string]bool{
	"assetdir":             true,
	"devserver":            true,
	"frontenddevserverurl": true,
	"loglevel":             true,
	"tsprefix":             true,
	"tssuffix":             true,
	"tsoutputtype":         true,
}

// parseTargets picks the file/directory paths out of a raw argument list
// (os.Args[1:]). It skips Wails' own flags and their values so only user
// targets remain, in the order given. Unknown dash-prefixed arguments are
// treated as valueless flags and dropped.
func parseTargets(args []string) []string {
	targets := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		if arg[0] != '-' {
			targets = append(targets, arg)
			continue
		}
		name := strings.TrimLeft(arg, "-")
		if strings.ContainsRune(name, '=') {
			continue // -flag=value carries its value inline
		}
		if wailsValueFlags[name] {
			i++ // consume the flag's separate value
		}
	}
	return targets
}

// InitialTargets returns the command-line file/directory paths, skipping
// Wails' own flags. It is empty when the app was launched without arguments,
// so the frontend keeps its normal empty state. The frontend feeds any paths
// through the same scan path as the "choose folder" button.
func (a *App) InitialTargets() []string {
	if a.initialTargets == nil {
		return []string{}
	}
	return a.initialTargets
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Native file drag & drop: forward the dropped paths to the frontend, which
	// runs the same scan path as the "choose" buttons.
	runtime.OnFileDrop(ctx, func(_, _ int, paths []string) {
		runtime.EventsEmit(ctx, "files:dropped", paths)
	})
}

// SetLanguage selects the language used for native dialogs. It accepts "zh"
// (default) or "en"; anything else falls back to "zh".
func (a *App) SetLanguage(lang string) {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "en":
		a.lang = "en"
	default:
		a.lang = "zh"
	}
}

// dialogText returns the open/save titles and filter name for the active language.
func (a *App) dialogText() (openTitle, saveTitle, filterName string) {
	if a.lang == "en" {
		return "Choose an MP4 file", "Save MP4", "Video"
	}
	return "选择 MP4 文件", "保存 MP4", "视频"
}

// folderTitle / outputDirTitle name the two directory pickers.
func (a *App) folderTitle() string {
	if a.lang == "en" {
		return "Choose a folder"
	}
	return "选择文件夹"
}

func (a *App) outputDirTitle() string {
	if a.lang == "en" {
		return "Choose the output folder"
	}
	return "选择输出目录"
}

// Probe diagnoses a file's container layout.
func (a *App) Probe(path string) (*probe.Report, error) {
	return probe.Analyze(path)
}

// ChooseInput opens a file dialog and returns the chosen path (empty if cancelled).
func (a *App) ChooseInput() (string, error) {
	openTitle, _, filterName := a.dialogText()
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   openTitle,
		Filters: []runtime.FileFilter{{DisplayName: filterName, Pattern: "*.mp4;*.m4v;*.mov"}},
	})
}

// ChooseOutput opens a save dialog and returns the chosen path (empty if cancelled).
func (a *App) ChooseOutput(defaultName string) (string, error) {
	_, saveTitle, _ := a.dialogText()
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           saveTitle,
		DefaultFilename: defaultName,
	})
}

// ChooseInputs opens a multi-select file dialog and returns the chosen paths
// (empty if cancelled).
func (a *App) ChooseInputs() ([]string, error) {
	openTitle, _, filterName := a.dialogText()
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   openTitle,
		Filters: []runtime.FileFilter{{DisplayName: filterName, Pattern: "*.mp4;*.m4v;*.mov"}},
	})
}

// ChooseFolder opens a directory picker (empty if cancelled).
func (a *App) ChooseFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: a.folderTitle()})
}

// ChooseOutputDir opens a directory picker for batch output (empty if cancelled).
func (a *App) ChooseOutputDir() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: a.outputDirTitle()})
}

// ScanPaths classifies the given files and/or directories and returns one
// verdict per video file, sorted by name. Unreadable paths come back as
// broken verdicts rather than an error, so one bad entry never hides a batch.
func (a *App) ScanPaths(paths []string) ([]classify.Verdict, error) {
	files, bad := classify.Collect(paths)
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
	return verdicts, nil
}

// NormalizeRequest describes a lossless normalization request.
type NormalizeRequest struct {
	Input      string `json:"input"`
	Output     string `json:"output"`
	Format     string `json:"format"` // "progressive" or "fmp4"
	WindowMs   int    `json:"windowMs"`
	FragmentMs int    `json:"fragmentMs"`
}

// ReencodeRequest describes an optional re-encode.
type ReencodeRequest struct {
	Input        string  `json:"input"`
	Output       string  `json:"output"`
	VideoCodec   string  `json:"videoCodec"`
	Hardware     string  `json:"hardware"`
	CRF          int     `json:"crf"`
	Preset       string  `json:"preset"`
	AudioBitrate string  `json:"audioBitrate"`
	GOPSeconds   float64 `json:"gopSeconds"`
}

// OpResult is returned by Normalize and Reencode.
type OpResult struct {
	Output     string           `json:"output"`
	InputSize  int64            `json:"inputSize"`
	OutputSize int64            `json:"outputSize"`
	Delta      int64            `json:"delta"`
	Changed    bool             `json:"changed"`
	Report     *probe.Report    `json:"report"`
	Progress   *ffmpeg.Progress `json:"progress,omitempty"`
}

// Normalize runs the lossless container pass (interleave or fMP4).
func (a *App) Normalize(req NormalizeRequest) (*OpResult, error) {
	out := req.Output
	if out == "" {
		out = defaultOut(req.Input, ".norm.mp4")
	}
	return a.transform(req.Input, out, func(src normalize.ReadSeekerAt, dst io.Writer) (*normalize.Result, error) {
		if strings.EqualFold(req.Format, "fmp4") {
			return normalize.Fragment(src, dst, normalize.FragmentOptions{FragmentMs: req.FragmentMs})
		}
		return normalize.Interleave(src, dst, normalize.InterleaveOptions{WindowMs: req.WindowMs})
	})
}

// BatchRequest describes a normalize-all run over the batch list.
type BatchRequest struct {
	Inputs     []string `json:"inputs"`
	Outdir     string   `json:"outdir"`
	Format     string   `json:"format"`
	WindowMs   int      `json:"windowMs"`
	FragmentMs int      `json:"fragmentMs"`
}

// BatchProgress is emitted on "batch:progress" as each file moves through the
// queue. Status is one of running, done, failed.
type BatchProgress struct {
	Index  int    `json:"index"`
	Total  int    `json:"total"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// BatchOutcome is the per-file result of a batch run.
type BatchOutcome struct {
	Path       string `json:"path"`
	Output     string `json:"output"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	InputSize  int64  `json:"inputSize,omitempty"`
	OutputSize int64  `json:"outputSize,omitempty"`
}

// BatchResult summarises a normalize-all run.
type BatchResult struct {
	Total     int            `json:"total"`
	Done      int            `json:"done"`
	Skipped   int            `json:"skipped"`
	Failed    int            `json:"failed"`
	Cancelled bool           `json:"cancelled"`
	Results   []BatchOutcome `json:"results"`
}

// BatchNormalize losslessly normalizes a whole list of files, one at a time,
// emitting "batch:progress" for the UI. Outputs default to a ".norm.mp4" file
// beside each input unless Outdir is set.
func (a *App) BatchNormalize(req BatchRequest) (*BatchResult, error) {
	if len(req.Inputs) == 0 {
		return nil, fmt.Errorf("batch: no input files")
	}
	a.batchMu.Lock()
	if a.batchCancel != nil {
		a.batchMu.Unlock()
		return nil, fmt.Errorf("batch: a run is already in progress")
	}
	base := context.Background()
	if a.ctx != nil {
		base = a.ctx
	}
	ctx, cancel := context.WithCancel(base)
	a.batchCancel = cancel
	a.batchMu.Unlock()
	defer func() {
		a.batchMu.Lock()
		a.batchCancel = nil
		a.batchMu.Unlock()
		cancel()
	}()

	fn, _, err := normalizeFunc(req.Format, req.WindowMs, req.FragmentMs)
	if err != nil {
		return nil, err
	}
	if req.Outdir != "" {
		if err := os.MkdirAll(req.Outdir, 0o755); err != nil {
			return nil, err
		}
	}

	res := &BatchResult{Total: len(req.Inputs)}
	for i, in := range req.Inputs {
		if ctx.Err() != nil {
			res.Cancelled = true
			res.Skipped = res.Total - i
			break
		}
		out := defaultBatchOut(in, req.Outdir)
		a.emitBatch(BatchProgress{Index: i + 1, Total: res.Total, Path: in, Status: "running"})

		or, err := a.transform(in, out, fn)
		if err != nil {
			res.Failed++
			res.Results = append(res.Results, BatchOutcome{Path: in, Output: out, Status: "failed", Error: err.Error()})
			a.emitBatch(BatchProgress{Index: i + 1, Total: res.Total, Path: in, Status: "failed", Error: err.Error()})
			continue
		}
		res.Done++
		res.Results = append(res.Results, BatchOutcome{
			Path: in, Output: out, Status: "done",
			InputSize: or.InputSize, OutputSize: or.OutputSize,
		})
		a.emitBatch(BatchProgress{Index: i + 1, Total: res.Total, Path: in, Status: "done"})
	}
	return res, nil
}

// CancelBatch stops a running normalize-all run after the current file.
func (a *App) CancelBatch() {
	a.batchMu.Lock()
	cancel := a.batchCancel
	a.batchMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) emitBatch(p BatchProgress) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "batch:progress", p)
	}
}

// normalizeFunc builds the lossless transform for the requested output format.
func normalizeFunc(format string, windowMs, fragMs int) (func(normalize.ReadSeekerAt, io.Writer) (*normalize.Result, error), bool, error) {
	switch strings.ToLower(format) {
	case "fmp4", "fragmented":
		return func(src normalize.ReadSeekerAt, dst io.Writer) (*normalize.Result, error) {
			return normalize.Fragment(src, dst, normalize.FragmentOptions{FragmentMs: fragMs})
		}, true, nil
	default:
		return func(src normalize.ReadSeekerAt, dst io.Writer) (*normalize.Result, error) {
			return normalize.Interleave(src, dst, normalize.InterleaveOptions{WindowMs: windowMs})
		}, false, nil
	}
}

// defaultBatchOut returns the output path for one batch input: alongside it by
// default, or inside outdir when set.
func defaultBatchOut(in, outdir string) string {
	base := strings.TrimSuffix(filepath.Base(in), filepath.Ext(in)) + ".norm.mp4"
	if outdir == "" {
		return filepath.Join(filepath.Dir(in), base)
	}
	return filepath.Join(outdir, base)
}

// Reencode runs the optional ffmpeg re-encode path, emitting progress events.
func (a *App) Reencode(req ReencodeRequest) (*OpResult, error) {
	out := req.Output
	if out == "" {
		out = defaultOut(req.Input, ".reenc.mp4")
	}
	opts := ffmpeg.Options{
		Video:        ffmpeg.VideoCodec(strings.ToLower(req.VideoCodec)),
		Hardware:     ffmpeg.HardwareMode(strings.ToLower(req.Hardware)),
		CRF:          req.CRF,
		Preset:       req.Preset,
		AudioBitrate: req.AudioBitrate,
		GOPSeconds:   req.GOPSeconds,
	}
	cfg := ffmpeg.Config{}
	if err := cfg.Run(a.ctx, req.Input, out, opts, func(p ffmpeg.Progress) {
		runtime.EventsEmit(a.ctx, "reencode:progress", p)
	}); err != nil {
		return nil, err
	}
	rep, err := probe.Analyze(out)
	if err != nil {
		return nil, err
	}
	inSize, outSize := fileSizes(req.Input, out)
	return &OpResult{Output: out, InputSize: inSize, OutputSize: outSize, Changed: true, Report: rep}, nil
}

// FFmpegVersion reports the detected ffmpeg, or an error if none is found.
func (a *App) FFmpegVersion() (string, error) {
	return ffmpeg.Config{}.Version(context.Background())
}

func (a *App) transform(input, output string, fn func(normalize.ReadSeekerAt, io.Writer) (*normalize.Result, error)) (*OpResult, error) {
	in, err := os.Open(input)
	if err != nil {
		return nil, err
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(output), ".mp4norm-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	res, err := fn(in, tmp)
	if err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpName, output); err != nil {
		return nil, err
	}

	rep, err := probe.Analyze(output)
	if err != nil {
		return nil, err
	}
	return &OpResult{
		Output:     output,
		InputSize:  res.InputSize,
		OutputSize: res.OutputSize,
		Delta:      res.Delta,
		Changed:    res.Changed,
		Report:     rep,
	}, nil
}

func defaultOut(in, suffix string) string {
	return strings.TrimSuffix(in, filepath.Ext(in)) + suffix
}

func fileSizes(a, b string) (int64, int64) {
	var sa, sb int64
	if fi, err := os.Stat(a); err == nil {
		sa = fi.Size()
	}
	if fi, err := os.Stat(b); err == nil {
		sb = fi.Size()
	}
	return sa, sb
}
