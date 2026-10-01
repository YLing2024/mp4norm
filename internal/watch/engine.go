package watch

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/YLing2024/mp4norm/internal/classify"
)

// Classifier returns the verdict for one file. It matches classify.ClassifyFile
// but is injectable so the engine can be tested without real media.
type Classifier func(path string) classify.Verdict

// Processor normalizes one file and returns the published output path. It is
// injectable so the engine can be tested without ffmpeg; the CLI wires it to
// safefile.Transform and prints the benefit summary itself.
type Processor func(path string) (output string, err error)

// Runner scans a single directory, decides which files are safe to process and
// normalizes them once. It owns no concurrency: the caller drives Pass or
// RunOnce.
type Runner struct {
	// Root is the directory being watched.
	Root string
	// State is the persisted state for Root.
	State *State
	// Classify inspects a stable file.
	Classify Classifier
	// Process normalizes a stable needs-work file.
	Process Processor
	// Log receives per-file notes (such as "still being written"). It is
	// suppressed in quiet mode.
	Log func(format string, args ...any)
	// Warn receives errors that should always be visible, even in quiet mode.
	Warn func(format string, args ...any)
	// Quiet limits Log to errors only.
	Quiet bool
	// Now is the clock, overridable in tests.
	Now func() time.Time
}

// Summary counts one pass.
type Summary struct {
	Scanned   int
	NeedsWork int
	Processed int
	Failed    int
	Writing   int
	Skipped   int
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Runner) log(format string, args ...any) {
	if r.Log != nil && !r.Quiet {
		r.Log(format, args...)
	}
}

func (r *Runner) warn(format string, args ...any) {
	if r.Warn != nil {
		r.Warn(format, args...)
	}
}

// Pass performs a single scan: it observes every candidate, defers files seen
// for the first time, skips files that are still changing, and processes the
// stable files that still need work. Files already done or failed with the
// same observation are left untouched (no retry).
func (r *Runner) Pass() Summary {
	var s Summary
	now := r.now()
	files, bad := Collect(r.Root)
	for _, b := range bad {
		r.warn("cannot scan %s: %v", b.Path, b.Err)
	}
	for _, path := range files {
		s.Scanned++
		cur, err := Stat(path)
		if err != nil {
			r.warn("cannot stat %s: %v", path, err)
			s.Failed++
			continue
		}
		switch r.State.Observe(path, cur) {
		case DecisionFirstSeen:
			// A brand-new file is not yet known to have finished being
			// written. Report it as writing and defer classification to a
			// later pass, so a half-written download is never judged.
			s.Writing++
			r.log("watching %s: first observation, will settle next pass", path)
			continue
		case DecisionWriting:
			s.Writing++
			r.log("skipping %s: still being written", path)
			continue
		}
		// The file is stable. A terminal result for this exact observation
		// means it was already handled, so it is not retried.
		if r.State.Settled(path) {
			continue
		}
		v := r.Classify(path)
		switch v.Status {
		case classify.StatusOK:
			r.State.Record(path, cur, ResultSkipped, now, "", "")
			s.Skipped++
		case classify.StatusBroken:
			reason := v.Error
			if reason == "" {
				reason = v.ReasonsText("en")
			}
			r.State.Record(path, cur, ResultFailed, now, "", reason)
			s.Failed++
			r.warn("broken %s: %s", path, reason)
		default:
			s.NeedsWork++
			out, err := r.Process(path)
			if err != nil {
				r.State.Record(path, cur, ResultFailed, now, "", err.Error())
				s.Failed++
				r.warn("fail %s: %v", path, err)
				continue
			}
			r.State.Record(path, cur, ResultDone, now, out, "")
			s.Processed++
		}
	}
	return s
}

// RunOnce is the body of `watch -once`: a silent observation pass records
// first sightings, then a full pass processes the files that are now known to
// be settled. The extra pass is what lets a single invocation handle a folder
// that watch has never seen, while a file that is still being appended changes
// between the two passes and is skipped.
func (r *Runner) RunOnce() Summary {
	r.Observe()
	return r.Pass()
}

// Observe updates the recorded (size, mtime) for every candidate without
// classifying, processing or logging. It primes the first pass of a -once run
// so a folder watch has never seen can be handled in one invocation.
func (r *Runner) Observe() {
	files, _ := Collect(r.Root)
	for _, path := range files {
		cur, err := Stat(path)
		if err != nil {
			continue
		}
		r.State.Observe(path, cur)
	}
}

// Collect lists the media candidates under root, newest scan last. Directories
// and files carrying the `.mp4norm-` prefix (backups, temp files, the state
// file itself) are skipped so the watcher never feeds its own artefacts back
// into the pipeline.
func Collect(root string) (files []string, bad []classify.Unreadable) {
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			bad = append(bad, classify.Unreadable{Path: path, Err: err})
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".mp4norm-") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(d.Name())) {
		case ".mp4", ".m4v", ".mov":
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		bad = append(bad, classify.Unreadable{Path: root, Err: err})
	}
	sort.Strings(files)
	return files, bad
}
