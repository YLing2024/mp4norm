// Package ffmpeg integrates an external ffmpeg binary for the optional
// re-encode path. Container-level normalization never needs it; it is only
// used when the source cannot be fixed by remuxing (e.g. very sparse keyframes
// or variable frame rate) and a real encoder is required.
package ffmpeg

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Config locates and runs an ffmpeg binary.
type Config struct {
	// Path is the ffmpeg executable. If empty, Locate is used.
	Path string
}

// Locate finds an ffmpeg binary. It checks, in order:
//
//  1. the MP4NORM_FFMPEG environment variable,
//  2. a binary bundled next to this executable (ffmpeg/ or ffmpeg/bin/),
//  3. PATH.
func Locate() (string, error) {
	if p := os.Getenv("MP4NORM_FFMPEG"); p != "" {
		if isExecutable(p) {
			return p, nil
		}
		return "", fmt.Errorf("MP4NORM_FFMPEG=%q is not an executable file", p)
	}

	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, rel := range []string{"ffmpeg", filepath.Join("ffmpeg", "bin"), "third_party/ffmpeg", filepath.Join("third_party", "ffmpeg", "bin")} {
			cand := filepath.Join(dir, rel, exeName("ffmpeg"))
			if isExecutable(cand) {
				return cand, nil
			}
		}
	}

	// Support a bundled binary in the working directory (portable/dev layout).
	if wd, err := os.Getwd(); err == nil {
		for _, rel := range []string{"ffmpeg", filepath.Join("ffmpeg", "bin"), "third_party/ffmpeg", filepath.Join("third_party", "ffmpeg", "bin")} {
			cand := filepath.Join(wd, rel, exeName("ffmpeg"))
			if isExecutable(cand) {
				return cand, nil
			}
		}
	}

	if p, err := exec.LookPath(exeName("ffmpeg")); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("ffmpeg not found (set MP4NORM_FFMPEG or install it on PATH)")
}

func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func (c Config) path() (string, error) {
	if c.Path != "" {
		return c.Path, nil
	}
	return Locate()
}

// Version runs "ffmpeg -version" and returns the first line.
func (c Config) Version(ctx context.Context) (string, error) {
	path, err := c.path()
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, path, "-version").Output()
	if err != nil {
		return "", fmt.Errorf("run ffmpeg -version: %w", err)
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(line), nil
}

// VideoCodec is the target video codec for re-encoding.
type VideoCodec string

const (
	// CodecH264 targets H.264/AVC.
	CodecH264 VideoCodec = "h264"
	// CodecH265 targets H.265/HEVC.
	CodecH265 VideoCodec = "h265"
	// CodecCopy copies the video stream without re-encoding.
	CodecCopy VideoCodec = "copy"
)

// HardwareMode controls hardware encoder preference.
type HardwareMode string

const (
	// HardwareAuto tries hardware encoders and falls back to software.
	HardwareAuto HardwareMode = "auto"
	// HardwareOnly requires a hardware encoder.
	HardwareOnly HardwareMode = "on"
	// HardwareOff forces the software encoder.
	HardwareOff HardwareMode = "off"
)

// Options describes a re-encode.
type Options struct {
	Video        VideoCodec
	Hardware     HardwareMode
	CRF          int    // quality: lower is better; ~18-28 for x264
	Preset       string // x264/x265 preset, e.g. "fast"
	AudioBitrate string // e.g. "192k"; empty keeps the audio stream (copy)
	GOPSeconds   float64
	ExtraArgs    []string
}

// Defaults fills unset fields with sensible values.
func (o Options) withDefaults() Options {
	if o.Video == "" {
		o.Video = CodecH264
	}
	if o.Hardware == "" {
		o.Hardware = HardwareAuto
	}
	if o.CRF <= 0 {
		o.CRF = 23
	}
	if o.Preset == "" {
		o.Preset = "medium"
	}
	if o.GOPSeconds <= 0 {
		o.GOPSeconds = 2
	}
	return o
}

var softwareEncoders = map[VideoCodec]string{
	CodecH264: "libx264",
	CodecH265: "libx265",
}

var hardwareCandidates = map[VideoCodec][]string{
	CodecH264: {"h264_nvenc", "h264_qsv", "h264_amf", "h264_videotoolbox"},
	CodecH265: {"hevc_nvenc", "hevc_qsv", "hevc_amf", "hevc_videotoolbox"},
}

// EncoderFor picks an encoder name for the requested codec and hardware mode.
// With HardwareAuto it probes candidate hardware encoders and falls back to the
// software encoder.
func (c Config) EncoderFor(ctx context.Context, o Options) (string, error) {
	o = o.withDefaults()
	if o.Video == CodecCopy {
		return "copy", nil
	}
	sw, ok := softwareEncoders[o.Video]
	if !ok {
		return "", fmt.Errorf("unsupported video codec %q", o.Video)
	}
	if o.Hardware == HardwareOff {
		return sw, nil
	}
	path, err := c.path()
	if err != nil {
		return "", err
	}
	for _, cand := range hardwareCandidates[o.Video] {
		if probeEncoder(ctx, path, cand) {
			return cand, nil
		}
	}
	if o.Hardware == HardwareOnly {
		return "", fmt.Errorf("no hardware %s encoder available", o.Video)
	}
	return sw, nil
}

// probeEncoder does a tiny test encode to check the encoder actually works
// (being listed by ffmpeg is not sufficient).
func probeEncoder(ctx context.Context, path, encoder string) bool {
	cmd := exec.CommandContext(ctx, path,
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=black:s=64x64:d=0.1:r=10",
		"-frames:v", "2", "-c:v", encoder, "-f", "null", "-",
	)
	return cmd.Run() == nil
}

// BuildArgs builds the ffmpeg argument list. It is pure and unit-testable.
func BuildArgs(src, dst string, o Options, encoder string) []string {
	o = o.withDefaults()

	args := []string{
		"-hide_banner", "-nostdin", "-loglevel", "error",
		"-progress", "pipe:1", "-nostats",
		"-y",
		"-i", src,
		"-map", "0:v:0?",
		"-map", "0:a:0?",
	}

	if encoder == "copy" {
		args = append(args, "-c", "copy")
	} else {
		args = append(args, videoArgs(encoder, o)...)
		if o.AudioBitrate == "" {
			args = append(args, "-c:a", "copy")
		} else {
			args = append(args, "-c:a", "aac", "-b:a", o.AudioBitrate)
		}
	}

	args = append(args, "-movflags", "+faststart")
	args = append(args, o.ExtraArgs...)
	args = append(args, dst)
	return args
}

func videoArgs(encoder string, o Options) []string {
	args := []string{"-c:v", encoder}
	switch {
	case strings.HasSuffix(encoder, "_nvenc"):
		args = append(args, "-rc", "vbr", "-cq", strconv.Itoa(o.CRF), "-b:v", "0", "-preset", nvencPreset(o.Preset))
	case strings.HasSuffix(encoder, "_qsv"):
		args = append(args, "-global_quality", strconv.Itoa(o.CRF))
	case strings.HasSuffix(encoder, "_videotoolbox"):
		args = append(args, "-q:v", strconv.Itoa(o.CRF))
	case strings.HasSuffix(encoder, "_amf"):
		args = append(args, "-rc", "cqp", "-qp_i", strconv.Itoa(o.CRF), "-qp_p", strconv.Itoa(o.CRF))
	default: // libx264 / libx265
		args = append(args, "-preset", o.Preset, "-crf", strconv.Itoa(o.CRF))
	}
	if o.GOPSeconds > 0 {
		// Force regular keyframes so seeking has frequent entry points.
		args = append(args, "-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%g)", o.GOPSeconds))
	}
	return args
}

func nvencPreset(preset string) string {
	switch preset {
	case "ultrafast", "superfast", "veryfast":
		return "p1"
	case "faster", "fast":
		return "p3"
	case "medium":
		return "p5"
	case "slow", "slower":
		return "p6"
	case "veryslow":
		return "p7"
	default:
		return "p5"
	}
}

// Progress is a snapshot of encode progress parsed from ffmpeg's -progress output.
type Progress struct {
	Frame   int64
	OutTime int64 // microseconds
	Speed   string
	Done    bool
}

// Run re-encodes src to dst, preferring a hardware encoder per o.Hardware.
// The optional callback receives progress updates.
func (c Config) Run(ctx context.Context, src, dst string, o Options, onProgress func(Progress)) error {
	path, err := c.path()
	if err != nil {
		return err
	}
	encoder, err := c.EncoderFor(ctx, o)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, path, BuildArgs(src, dst, o, encoder)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	if onProgress != nil {
		var p Progress
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			parseInto(sc.Text(), &p)
			onProgress(p)
		}
	} else {
		_, _ = bufio.NewReader(stdout).WriteTo(discard{})
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// parseInto parses one key=value line of ffmpeg -progress output into p.
func parseInto(line string, p *Progress) {
	key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
	if !ok {
		return
	}
	switch key {
	case "frame":
		if n, err := strconv.ParseInt(val, 10, 64); err == nil {
			p.Frame = n
		}
	case "out_time_us", "out_time_ms":
		if n, err := strconv.ParseInt(val, 10, 64); err == nil {
			p.OutTime = n
		}
	case "speed":
		p.Speed = val
	case "progress":
		p.Done = val == "end"
	}
}
