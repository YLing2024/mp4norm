package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/YLing2024/mp4norm/internal/watch"
)

func TestWatchRejectsInPlaceWithOutdir(t *testing.T) {
	dir := t.TempDir()
	err := runWatch([]string{"-once", "-in-place", "-outdir", filepath.Join(dir, "out"), dir})
	if err == nil {
		t.Fatal("runWatch -in-place -outdir: want a mutual-exclusion error")
	}
}

func TestWatchRejectsBackupDirWithoutInPlace(t *testing.T) {
	dir := t.TempDir()
	if err := runWatch([]string{"-once", "-backup-dir", filepath.Join(dir, "bk"), dir}); err == nil {
		t.Fatal("runWatch -backup-dir without -in-place: want an error")
	}
}

func TestWatchRejectsIntervalBelowMinimum(t *testing.T) {
	dir := t.TempDir()
	if err := runWatch([]string{"-interval", "1s", dir}); err == nil {
		t.Fatal("runWatch -interval 1s: want a minimum-interval error")
	}
}

func TestWatchRequiresDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runWatch([]string{"-once", file}); err == nil {
		t.Fatal("runWatch on a plain file: want an error")
	}
}

func TestWatchRequiresDirectoryArg(t *testing.T) {
	if err := runWatch([]string{"-once"}); err == nil {
		t.Fatal("runWatch without directories: want an error")
	}
}

func TestWatchOnceFirstRoundIsBaseline(t *testing.T) {
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(clip, make([]byte, 20000), 0o644); err != nil {
		t.Fatal(err)
	}
	// A freshly seen file is only observed: nothing is processed and, even
	// though it is not a valid MP4 yet, it is not recorded as failed.
	if err := runWatch([]string{"-once", "-quiet", dir}); err != nil {
		t.Fatalf("first -once round on a new file: %v", err)
	}
	// The file changes (as a download would): still just writing.
	if err := os.WriteFile(clip, make([]byte, 59976), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runWatch([]string{"-once", "-quiet", dir}); err != nil {
		t.Fatalf("second -once round on a growing file: %v", err)
	}
	// Once it settles, the broken verdict is allowed to stick (exit code 2).
	err := runWatch([]string{"-once", "-quiet", dir})
	ec, ok := err.(exitCodeError)
	if !ok || ec.code != 2 {
		t.Fatalf("settled -once round error = %v, want exit code 2", err)
	}
}

func TestWatchExitCode(t *testing.T) {
	cases := []struct {
		s    watch.Summary
		want int
	}{
		{watch.Summary{}, 0},
		{watch.Summary{Scanned: 3, Skipped: 3}, 0},
		{watch.Summary{Processed: 1, NeedsWork: 1}, 1},
		{watch.Summary{Processed: 1, Failed: 1, NeedsWork: 2}, 2},
		{watch.Summary{Failed: 1}, 2},
	}
	for _, c := range cases {
		if got := watchExitCode(c.s); got != c.want {
			t.Fatalf("watchExitCode(%+v) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestWatchOutputNaming(t *testing.T) {
	if got := watchOutput(filepath.Join("in", "clip.mp4"), ""); got != filepath.Join("in", "clip.norm.mp4") {
		t.Fatalf("watchOutput beside input = %q", got)
	}
	if got := watchOutput(filepath.Join("in", "clip.mov"), "out"); got != filepath.Join("out", "clip.norm.mp4") {
		t.Fatalf("watchOutput outdir = %q", got)
	}
}
