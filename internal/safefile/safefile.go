// Package safefile is the write path for every lossless rewrite. It streams the
// transform into a temporary file in the destination directory, verifies the
// product, and only then publishes it — either as a new file or, in in-place
// mode, over the original after an atomic backup. A failure at any point leaves
// the original file byte-for-byte untouched.
package safefile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/YLing2024/mp4norm/internal/backup"
	"github.com/YLing2024/mp4norm/internal/isobmff"
	"github.com/YLing2024/mp4norm/internal/normalize"
	"github.com/YLing2024/mp4norm/internal/probe"
	"github.com/YLing2024/mp4norm/internal/verify"
)

// TempPrefix names the temporary files a rewrite streams into the target
// directory. os.CreateTemp and the stale-file cleanup share this one constant,
// so the naming rule can never drift between where residue is written and
// where it is swept.
const TempPrefix = ".mp4norm-tmp-"

// StaleTempAfter is how long a temporary file may sit untouched in a target
// directory before a later rewrite treats it as residue from a process that
// was killed. A concurrently running instance keeps its own temp file's mtime
// fresh, so it is never swept by mistake.
const StaleTempAfter = 60 * time.Minute

// Temp describes one temporary file found in a target directory.
type Temp struct {
	// Path is the full path of the temp file.
	Path string
	// Size is its byte size when observed.
	Size int64
	// ModTime is when it was last written.
	ModTime time.Time
}

// ListTempFiles returns the rewrite temp files present in dir, oldest first. A
// directory that cannot be read yields no files, which leaves a best-effort
// cleanup with nothing to do about it.
func ListTempFiles(dir string) []Temp {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var temps []Temp
	for _, de := range des {
		if de.IsDir() || !strings.HasPrefix(de.Name(), TempPrefix) {
			continue
		}
		fi, err := de.Info()
		if err != nil {
			continue
		}
		temps = append(temps, Temp{Path: filepath.Join(dir, de.Name()), Size: fi.Size(), ModTime: fi.ModTime()})
	}
	sort.SliceStable(temps, func(i, j int) bool {
		if !temps[i].ModTime.Equal(temps[j].ModTime) {
			return temps[i].ModTime.Before(temps[j].ModTime)
		}
		return temps[i].Path < temps[j].Path
	})
	return temps
}

// CleanStaleTemps removes the temp files in dir whose last write is at least
// maxAge ago and returns the files it removed. It is deliberately best-effort:
// a file that cannot be removed is reported through logf and skipped, and a
// directory that cannot be read is a no-op. logf may be nil.
func CleanStaleTemps(dir string, maxAge time.Duration, logf func(format string, args ...any)) []Temp {
	now := time.Now()
	var removed []Temp
	for _, t := range ListTempFiles(dir) {
		if now.Sub(t.ModTime) < maxAge {
			continue
		}
		if err := os.Remove(t.Path); err != nil {
			if logf != nil {
				logf("stale temp not removed: %s (%s): %v", t.Path, probe.HumanBytes(t.Size), err)
			}
			continue
		}
		removed = append(removed, t)
		if logf != nil {
			logf("removed stale temp %s (%s, %.0f min old)",
				t.Path, probe.HumanBytes(t.Size), now.Sub(t.ModTime).Minutes())
		}
	}
	return removed
}

// Request describes one lossless rewrite.
type Request struct {
	// Input is the source media file.
	Input string
	// Output is the new-file target. Ignored when InPlace is set.
	Output string
	// InPlace replaces Input after backing it up.
	InPlace bool
	// BackupDir customizes where the backup goes (in-place only). Empty means
	// the default directory beside the input.
	BackupDir string
	// Fragmented selects the expected product layout for self-verification.
	Fragmented bool
	// Transform is the lossless rewrite to run.
	Transform func(normalize.ReadSeekerAt, io.Writer) (*normalize.Result, error)
	// Logf, when set, receives best-effort cleanup notes, such as stale temp
	// files removed before the rewrite. Cleanup failures are logged, never
	// returned, so they cannot abort the rewrite. May be nil.
	Logf func(format string, args ...any)
	// TrackTemp, when set, is called with the temp file path as soon as it is
	// created and again with an empty string once that file has been published
	// or removed. A long-lived host (the GUI) keeps the path so it can delete
	// an in-flight temp file if it exits before the rewrite finishes. It is not
	// safe for concurrent rewrites; a host that runs them in parallel must
	// serialize the callback itself.
	TrackTemp func(path string)
}

// Outcome is the successful result of a rewrite.
type Outcome struct {
	// Output is the published path (the input path for in-place mode).
	Output string
	// Result carries the rewrite statistics.
	Result *normalize.Result
	// Backup is the entry created for the original in in-place mode.
	Backup *backup.Entry
	// BackupPath is the on-disk path of the backup (in-place mode only).
	BackupPath string
	// Before and After are the probe reports used for the benefit summary.
	Before *probe.Report
	After  *probe.Report
}

// Transform runs req and publishes the verified product.
func Transform(req Request) (*Outcome, error) {
	if req.Input == "" {
		return nil, fmt.Errorf("no input file")
	}
	if req.Transform == nil {
		return nil, fmt.Errorf("no transform")
	}
	if !req.InPlace && req.Output == "" {
		return nil, fmt.Errorf("no output path")
	}

	// Read-only probe of the original, used only for the benefit summary. A
	// file the transform can handle is always probeable, so an error here is
	// non-fatal.
	before, _ := probe.Analyze(req.Input)

	in, err := os.Open(req.Input)
	if err != nil {
		return nil, err
	}
	src, err := validExtent(in)
	if err != nil {
		in.Close()
		return nil, err
	}

	targetDir := filepath.Dir(req.Output)
	if req.InPlace {
		targetDir = filepath.Dir(req.Input)
	}

	// Sweep residue from an earlier run that was killed before its deferred
	// cleanup could run. Fresh temp files (a concurrent instance) are left
	// alone because their mtime is newer than StaleTempAfter. Best-effort by
	// design: a failed sweep must never stop the rewrite that follows.
	CleanStaleTemps(targetDir, StaleTempAfter, req.Logf)

	tmp, err := os.CreateTemp(targetDir, TempPrefix+"*")
	if err != nil {
		in.Close()
		return nil, err
	}
	tmpName := tmp.Name()
	if req.TrackTemp != nil {
		req.TrackTemp(tmpName)
	}
	// No-op once the temp file has been renamed into place. Reporting the empty
	// path afterwards lets the host forget it.
	defer func() {
		os.Remove(tmpName)
		if req.TrackTemp != nil {
			req.TrackTemp("")
		}
	}()

	res, err := req.Transform(src, tmp)
	// Release the source handle before any rename: Windows refuses to rename a
	// file that is still open.
	closeErr := in.Close()
	if err != nil {
		tmp.Close()
		return nil, err
	}
	if closeErr != nil {
		tmp.Close()
		return nil, closeErr
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}

	// Never trust our own output. A failed check removes the product and
	// leaves the original alone.
	if err := verify.Product(req.Input, tmpName, req.Fragmented); err != nil {
		os.Remove(tmpName)
		return nil, fmt.Errorf("self-check failed: %w", err)
	}

	if !req.InPlace {
		if err := os.Rename(tmpName, req.Output); err != nil {
			return nil, err
		}
		after, _ := probe.Analyze(req.Output)
		return &Outcome{Output: req.Output, Result: res, Before: before, After: after}, nil
	}

	dir := req.BackupDir
	if dir == "" {
		dir = backup.DefaultDir(req.Input)
	}
	entry, err := backup.Move(req.Input, dir)
	if err != nil {
		return nil, fmt.Errorf("back up original: %w", err)
	}

	if err := os.Rename(tmpName, req.Input); err != nil {
		// Roll the backup back so the original is exactly where it was.
		stored := filepath.Join(dir, entry.Backup)
		if rbErr := os.Rename(stored, req.Input); rbErr != nil {
			return nil, fmt.Errorf(
				"replace failed: %v; rollback also failed (%v), the untouched original is preserved at %s",
				err, rbErr, stored)
		}
		return nil, fmt.Errorf("replace failed, original restored: %w", err)
	}

	after, _ := probe.Analyze(req.Input)
	return &Outcome{
		Output:     req.Input,
		Result:     res,
		Backup:     &entry,
		BackupPath: filepath.Join(dir, entry.Backup),
		Before:     before,
		After:      after,
	}, nil
}

// validExtent returns a read view of f limited to its well-formed top-level
// boxes. Trailing bytes that do not form a box (a malformed final box or a
// short residue) are hidden so that strict MP4 decoders — mp4ff in particular —
// accept files that our own scanner already treats as valid. The lossless
// transforms never copy those bytes, so the output is unaffected.
func validExtent(f *os.File) (normalize.ReadSeekerAt, error) {
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	boxes, trunc, err := isobmff.ScanTopLevel(f, fi.Size())
	if err != nil {
		return nil, err
	}
	end := fi.Size()
	if trunc != nil {
		// Last box claims more than exists; keep only the boxes before it.
		end = trunc.Offset
	} else if len(boxes) > 0 {
		end = boxes[len(boxes)-1].End()
	}
	if end >= fi.Size() || end <= 0 {
		return f, nil
	}
	return io.NewSectionReader(f, 0, end), nil
}
