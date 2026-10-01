package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadStateMissingIsEmpty(t *testing.T) {
	s, err := LoadState(filepath.Join(t.TempDir(), StateFileName))
	if err != nil {
		t.Fatalf("LoadState missing: %v", err)
	}
	if len(s.Files) != 0 {
		t.Fatalf("files = %v, want empty", s.Files)
	}
}

func TestLoadStateCorruptReturnsEmptyWithWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFileName)
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadState(path)
	if err == nil {
		t.Fatal("LoadState corrupt: want a warning error, got nil")
	}
	if len(s.Files) != 0 {
		t.Fatalf("files = %v, want empty after corruption", s.Files)
	}
}

func TestStateSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFileName)
	cur := SizeMtime{Size: 42, Mtime: 7}
	at := time.Unix(1700000000, 0).UTC()
	s := NewState()
	s.Record("a.mp4", cur, ResultDone, at, "a.norm.mp4", "")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	fs, ok := got.Files["a.mp4"]
	if !ok {
		t.Fatalf("files = %v, want a.mp4", got.Files)
	}
	if fs.Observed != cur || fs.Result != ResultDone || fs.Output != "a.norm.mp4" || !fs.Time.Equal(at) {
		t.Fatalf("round trip = %+v, want observed=%+v result=%s output=a.norm.mp4 time=%s",
			fs, cur, ResultDone, at)
	}
}

func TestObserveFirstSeenThenStable(t *testing.T) {
	s := NewState()
	cur := SizeMtime{Size: 1, Mtime: 1}
	if got := s.Observe("a.mp4", cur); got != DecisionFirstSeen {
		t.Fatalf("first Observe = %v, want first seen", got)
	}
	if got := s.Observe("a.mp4", cur); got != DecisionStable {
		t.Fatalf("second Observe = %v, want stable", got)
	}
}

func TestObserveChangeClearsTerminalResult(t *testing.T) {
	s := NewState()
	cur := SizeMtime{Size: 1, Mtime: 1}
	s.Observe("a.mp4", cur)
	s.Record("a.mp4", cur, ResultFailed, time.Now(), "", "boom")
	if !s.Settled("a.mp4") {
		t.Fatal("failed and unchanged must be settled (no retry)")
	}
	if got := s.Observe("a.mp4", SizeMtime{Size: 2, Mtime: 2}); got != DecisionWriting {
		t.Fatalf("changed Observe = %v, want writing", got)
	}
	if s.Settled("a.mp4") {
		t.Fatal("a changed file must be re-evaluated, not settled")
	}
}

func TestSettledOnlyForTerminalResults(t *testing.T) {
	s := NewState()
	cur := SizeMtime{Size: 1, Mtime: 1}
	s.Observe("a.mp4", cur)
	if s.Settled("a.mp4") {
		t.Fatal("a newly observed file must not be settled")
	}
	s.Record("a.mp4", cur, ResultSkipped, time.Now(), "", "")
	if s.Settled("a.mp4") {
		t.Fatal("skipped is not a terminal result that blocks classification")
	}
}
