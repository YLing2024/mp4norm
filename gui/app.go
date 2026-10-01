package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/YLing2024/mp4norm/internal/ffmpeg"
	"github.com/YLing2024/mp4norm/internal/normalize"
	"github.com/YLing2024/mp4norm/internal/probe"
)

// App is the Wails application, exposing the mp4norm kernel to the frontend.
type App struct {
	ctx  context.Context
	lang string
}

// NewApp creates a new App.
func NewApp() *App { return &App{lang: "zh"} }

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

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
