package watch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YLing2024/mp4norm/internal/classify"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func fakeClassify(path string) classify.Verdict {
	base := filepath.Base(path)
	v := classify.Verdict{Path: path, Name: base}
	switch {
	case strings.Contains(base, "ok"):
		v.Status = classify.StatusOK
	case strings.Contains(base, "bad"):
		v.Status = classify.StatusBroken
		v.Error = "not a valid MP4"
	default:
		v.Status = classify.StatusNeedsWork
	}
	return v
}

type fakeProc struct {
	processed []string
}

func (f *fakeProc) run(path string) (string, error) {
	f.processed = append(f.processed, filepath.Base(path))
	return path + ".norm.mp4", nil
}

func newRunner(root string, proc Processor) *Runner {
	return &Runner{
		Root:     root,
		State:    NewState(),
		Classify: fakeClassify,
		Process:  proc,
		Warn:     func(string, ...any) {},
	}
}

func TestBaselineThenProcessStableNeedsWork(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.mp4", "A")
	writeFile(t, dir, "ok.mp4", "O")
	writeFile(t, dir, "bad.mp4", "B")

	proc := &fakeProc{}
	r := newRunner(dir, proc.run)

	// The first pass only observes: everything is reported as writing.
	if s := r.Pass(); s.Writing != 3 || s.Processed != 0 || s.Failed != 0 {
		t.Fatalf("baseline pass writing=%d processed=%d failed=%d, want 3/0/0", s.Writing, s.Processed, s.Failed)
	}
	s := r.Pass()
	if s.Processed != 1 || s.NeedsWork != 1 {
		t.Fatalf("processed=%d needsWork=%d, want 1/1", s.Processed, s.NeedsWork)
	}
	if s.Skipped != 1 {
		t.Fatalf("skipped=%d, want 1 (the ok file)", s.Skipped)
	}
	if s.Failed != 1 {
		t.Fatalf("failed=%d, want 1 (the broken file)", s.Failed)
	}
	if len(proc.processed) != 1 || proc.processed[0] != "a.mp4" {
		t.Fatalf("processed = %v, want [a.mp4]", proc.processed)
	}
}

func TestFirstPassIsBaselineAndNeverFails(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.mp4", "B") // would classify as broken if inspected

	proc := &fakeProc{}
	r := newRunner(dir, proc.run)
	s := r.Pass()

	if s.Writing != 1 || s.Failed != 0 || s.Processed != 0 {
		t.Fatalf("baseline pass writing=%d failed=%d processed=%d, want 1/0/0", s.Writing, s.Failed, s.Processed)
	}
	if len(proc.processed) != 0 {
		t.Fatalf("baseline pass processed %v, want none", proc.processed)
	}
}

func TestGrowingBrokenFileIsWritingNotFailed(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "bad.mp4", "B")

	proc := &fakeProc{}
	r := newRunner(dir, proc.run)
	r.Pass() // baseline observation

	if err := os.WriteFile(p, []byte("B plus more"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := r.Pass(); s.Writing != 1 || s.Failed != 0 {
		t.Fatalf("changed pass writing=%d failed=%d, want 1/0", s.Writing, s.Failed)
	}
	// Only once the file settles is the broken verdict allowed to stick.
	if s := r.Pass(); s.Failed != 1 {
		t.Fatalf("settled broken pass failed=%d, want 1", s.Failed)
	}
}

func TestPassSkipsFileThatIsStillBeingWritten(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "w.mp4", "start")

	var logs []string
	proc := &fakeProc{}
	r := newRunner(dir, proc.run)
	r.Log = func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }

	r.Pass() // first sight: recorded, not processed
	if err := os.WriteFile(p, []byte("start plus more"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := r.Pass() // changed since last pass

	if s.Writing != 1 || s.Processed != 0 || s.Failed != 0 {
		t.Fatalf("writing=%d processed=%d failed=%d, want 1/0/0", s.Writing, s.Processed, s.Failed)
	}
	if len(logs) == 0 || !strings.Contains(logs[len(logs)-1], "still being written") {
		t.Fatalf("logs = %v, want a 'still being written' note", logs)
	}
	// Once writing stops, the next pass processes it.
	s = r.Pass()
	if s.Processed != 1 {
		t.Fatalf("processed=%d after write stopped, want 1", s.Processed)
	}
}

func TestFailedFileIsNotRetried(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.mp4", "A")

	attempts := 0
	r := newRunner(dir, func(string) (string, error) {
		attempts++
		return "", errors.New("boom")
	})
	r.Pass() // first sight
	if s := r.Pass(); s.Failed != 1 {
		t.Fatalf("failed=%d, want 1", s.Failed)
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d, want 1", attempts)
	}
	r.Pass() // unchanged failure: must not retry
	if attempts != 1 {
		t.Fatalf("attempts=%d after a settled failure, want 1 (no retry)", attempts)
	}
}

func TestChangedFileResetsFailureAndIsRetried(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.mp4", "A")

	attempts := 0
	r := newRunner(dir, func(string) (string, error) {
		attempts++
		return "", errors.New("boom")
	})
	r.Pass() // first sight
	r.Pass() // fails
	if err := os.WriteFile(p, []byte("A replaced"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.Pass() // changed: still writing, result cleared
	if s := r.Pass(); s.NeedsWork != 1 {
		t.Fatalf("needsWork=%d after replacement, want 1 (retry)", s.NeedsWork)
	}
	if attempts != 2 {
		t.Fatalf("attempts=%d, want 2 (retried after change)", attempts)
	}
}

func TestStateMakesSecondRunIdempotent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.mp4", "A")
	statePath := StatePath(dir)

	attempts := 0
	proc := func(string) (string, error) { attempts++; return "out.mp4", nil }

	r1 := newRunner(dir, proc)
	r1.State, _ = LoadState(statePath)
	r1.Pass() // baseline observation
	if s := r1.Pass(); s.Processed != 1 {
		t.Fatalf("first run processed=%d, want 1", s.Processed)
	}
	if err := r1.State.Save(statePath); err != nil {
		t.Fatal(err)
	}

	r2 := newRunner(dir, proc)
	r2.State, _ = LoadState(statePath)
	s := r2.Pass()
	if s.NeedsWork != 0 || s.Processed != 0 {
		t.Fatalf("second run needsWork=%d processed=%d, want 0/0", s.NeedsWork, s.Processed)
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d, want 1 (state suppressed the reprocess)", attempts)
	}
}

func TestCorruptStateStillProcessesWithWarning(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.mp4", "A")
	if err := os.WriteFile(StatePath(dir), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(StatePath(dir))
	if err == nil {
		t.Fatal("LoadState corrupt: want a warning")
	}
	r := newRunner(dir, (&fakeProc{}).run)
	r.State = st
	r.Pass() // baseline observation
	if s := r.Pass(); s.Processed != 1 {
		t.Fatalf("processed=%d after corrupt state, want 1", s.Processed)
	}
}

func TestCollectSkipsArtefacts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.mp4", "A")
	if err := os.Mkdir(filepath.Join(dir, ".mp4norm-backup"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".mp4norm-backup"), "old.mp4", "OLD")
	writeFile(t, dir, StateFileName, "{}")

	files, bad := Collect(dir)
	if len(bad) != 0 {
		t.Fatalf("bad = %v, want none", bad)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "a.mp4" {
		t.Fatalf("files = %v, want only a.mp4", files)
	}
}
