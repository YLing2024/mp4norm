package ffmpeg

import (
	"context"
	"strings"
	"testing"
)

func contains(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestBuildArgsSoftware(t *testing.T) {
	args := BuildArgs("in.mp4", "out.mp4", Options{Video: CodecH264}, "libx264")
	joined := strings.Join(args, " ")
	for _, want := range []string{"-c:v libx264", "-crf 23", "-preset medium", "-force_key_frames expr:gte(t,n_forced*2)", "-movflags +faststart", "-map 0:v:0?", "-map 0:a:0?", "-c:a copy"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q:\n%s", want, joined)
		}
	}
	if args[len(args)-1] != "out.mp4" {
		t.Errorf("output not last: %v", args)
	}
}

func TestBuildArgsNvenc(t *testing.T) {
	args := BuildArgs("in.mp4", "out.mp4", Options{Video: CodecH264, Hardware: HardwareAuto, CRF: 20, Preset: "fast"}, "h264_nvenc")
	joined := strings.Join(args, " ")
	for _, want := range []string{"-c:v h264_nvenc", "-rc vbr", "-cq 20", "-b:v 0", "-preset p3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q:\n%s", want, joined)
		}
	}
}

func TestBuildArgsCopy(t *testing.T) {
	args := BuildArgs("in.mp4", "out.mp4", Options{Video: CodecCopy}, "copy")
	if !contains(args, "copy") || !contains(args, "-c") {
		t.Errorf("copy mode args wrong: %v", args)
	}
	if contains(args, "-c:v") {
		t.Errorf("copy mode should not set -c:v: %v", args)
	}
}

func TestNvencPreset(t *testing.T) {
	cases := map[string]string{"veryfast": "p1", "fast": "p3", "medium": "p5", "slow": "p6", "veryslow": "p7", "bogus": "p5"}
	for in, want := range cases {
		if got := nvencPreset(in); got != want {
			t.Errorf("nvencPreset(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseInto(t *testing.T) {
	var p Progress
	parseInto("frame=120", &p)
	parseInto("out_time_us=4000000", &p)
	parseInto("speed=2.5x", &p)
	parseInto("progress=end", &p)
	if p.Frame != 120 || p.OutTime != 4000000 || p.Speed != "2.5x" || !p.Done {
		t.Errorf("parsed progress = %+v", p)
	}
}

// TestEncoderForSoftware exercises the pure software selection path.
func TestEncoderForSoftware(t *testing.T) {
	enc, err := Config{}.EncoderFor(context.Background(), Options{Video: CodecH264, Hardware: HardwareOff})
	if err != nil {
		t.Fatal(err)
	}
	if enc != "libx264" {
		t.Errorf("encoder = %q, want libx264", enc)
	}
}
