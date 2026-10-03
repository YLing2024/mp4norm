package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YLing2024/mp4norm/internal/classify"
)

// TestUsageIsChinese locks the user-facing contract: the help is Chinese apart
// from the command and flag names.
func TestUsageIsChinese(t *testing.T) {
	var buf bytes.Buffer
	usage(&buf)
	text := buf.String()

	for _, want := range []string{"命令：", "高级命令：", "check", "fix", "watch", "-in-place"} {
		if !strings.Contains(text, want) {
			t.Errorf("help is missing %q", want)
		}
	}
	for _, bad := range []string{"Usage:", "Commands:", "Advanced commands:", "Show this help"} {
		if strings.Contains(text, bad) {
			t.Errorf("help still contains English %q", bad)
		}
	}
}

func TestCommandErrorsAreChinese(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"fix without input", runFix(nil), "fix: 请给出要处理的文件或目录"},
		{"fix in-place with outdir", runFix([]string{"-in-place", "-outdir", "out", "x.mp4"}), "-in-place 与 -outdir（以及 -o）不能同时使用"},
		{"fix backup-dir without in-place", runFix([]string{"-backup-dir", "bk", "x.mp4"}), "-backup-dir 需要配合 -in-place 使用"},
		{"check without input", runScan(nil), "check: 请给出要检查的文件或目录"},
		{"probe without input", runProbe(nil), "probe: 请给出要检查的文件"},
		{"unknown command", run([]string{"nope"}), `未知命令 "nope"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err == nil {
				t.Fatalf("want an error containing %q, got nil", tc.want)
			}
			if !strings.Contains(tc.err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", tc.err, tc.want)
			}
		})
	}

	if _, _, err := normalizeFunc("bogus", 0, 0); err == nil || !strings.Contains(err.Error(), "未知格式") {
		t.Fatalf("normalizeFunc(bogus) = %v, want a 未知格式 error", err)
	}
}

// TestHumanizeErrorTurnsRawOSErrorsIntoChinese checks that a raw filesystem
// error never reaches the user, and that surrounding context survives.
func TestHumanizeErrorTurnsRawOSErrorsIntoChinese(t *testing.T) {
	notFound := &fs.PathError{Op: "GetFileAttributesEx", Path: `H:\no\such.mp4`, Err: fs.ErrNotExist}
	got := humanizeError(notFound)
	if !strings.Contains(got.Error(), "找不到文件") || !strings.Contains(got.Error(), `H:\no\such.mp4`) {
		t.Fatalf("humanizeError(not found) = %q", got)
	}
	if strings.Contains(got.Error(), "GetFileAttributesEx") {
		t.Fatalf("humanizeError leaked the raw call: %q", got)
	}

	wrapped := humanizeError(fmt.Errorf("back up original: %w", &fs.PathError{Path: "x.mp4", Err: fs.ErrPermission}))
	if wrapped.Error() != "back up original: 没有权限读取：x.mp4" {
		t.Fatalf("wrapped error = %q", wrapped)
	}

	// A suffix after the cause must be preserved (reencodeAdvice uses one).
	suffix := humanizeError(fmt.Errorf("%w\n  试试重编码", notFound))
	if !strings.HasPrefix(suffix.Error(), "找不到文件") || !strings.HasSuffix(suffix.Error(), "试试重编码") {
		t.Fatalf("suffixed error = %q", suffix)
	}
}

func TestScanNextStepMatchesInputType(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := scanNextStep([]string{file}); got != "下一步：mp4norm fix <文件>" {
		t.Fatalf("single file hint = %q", got)
	}
	if got := scanNextStep([]string{dir}); !strings.Contains(got, "-outdir") {
		t.Fatalf("directory hint = %q, want the -outdir form", got)
	}
	if got := scanNextStep([]string{file, file}); !strings.Contains(got, "-outdir") {
		t.Fatalf("multi-input hint = %q, want the -outdir form", got)
	}
}

func TestPrintScanReportShowsNextStepOnlyWhenNeeded(t *testing.T) {
	verdict := classify.Verdict{Name: "clip.mp4", Status: classify.StatusNeedsWork, Size: 10}

	var work bytes.Buffer
	printScanReport(&work, "clip.mp4", []classify.Verdict{verdict}, false, false, "下一步：mp4norm fix <文件>")
	if !strings.Contains(work.String(), "下一步：mp4norm fix <文件>") {
		t.Fatalf("needs-work report missing next step:\n%s", work.String())
	}

	clean := classify.Verdict{Name: "clip.mp4", Status: classify.StatusOK, Size: 10}
	var done bytes.Buffer
	printScanReport(&done, "clip.mp4", []classify.Verdict{clean}, false, false, "下一步：mp4norm fix <文件>")
	if strings.Contains(done.String(), "下一步") {
		t.Fatalf("clean report should not show a next step:\n%s", done.String())
	}
}
