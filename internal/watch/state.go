package watch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// StateFileName is the per-directory state file. It reuses the `.mp4norm-`
// prefix so it is never confused with media and can be excluded from scans by
// the same rule.
const StateFileName = ".mp4norm-watch-state.json"

const stateVersion = 1

// Result is the terminal outcome recorded for one file.
type Result string

const (
	// ResultDone means the file was normalized successfully.
	ResultDone Result = "done"
	// ResultSkipped means the file was inspected and did not need work.
	ResultSkipped Result = "skipped"
	// ResultFailed means processing (or classification) failed; it is not
	// retried until the file changes.
	ResultFailed Result = "failed"
)

// FileState is what the watcher last saw and did for one file.
type FileState struct {
	Observed SizeMtime `json:"observed"`
	Result   Result    `json:"result,omitempty"`
	Time     time.Time `json:"time,omitempty"`
	Output   string    `json:"output,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// State is the persisted watch state for one scanned directory.
type State struct {
	Version int                  `json:"version"`
	Files   map[string]FileState `json:"files"`
}

// NewState returns an empty state ready to use.
func NewState() *State {
	return &State{Version: stateVersion, Files: map[string]FileState{}}
}

// StatePath returns the state file that belongs to a scanned directory.
func StatePath(dir string) string { return filepath.Join(dir, StateFileName) }

// LoadState reads the state file at path. A missing file yields empty state and
// no error. A corrupt file also yields empty state, but the returned error
// describes the corruption so the caller can warn and carry on rather than
// crash.
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return NewState(), nil
		}
		return NewState(), err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return NewState(), fmt.Errorf("state file %s is corrupt (ignored): %w", path, err)
	}
	if s.Files == nil {
		s.Files = map[string]FileState{}
	}
	if s.Version == 0 {
		s.Version = stateVersion
	}
	return &s, nil
}

// Save writes the state atomically: it marshals to a temp file in the same
// directory and renames it over the target, so a crash mid-write cannot corrupt
// the live state file.
func (s *State) Save(path string) error {
	s.Version = stateVersion
	if s.Files == nil {
		s.Files = map[string]FileState{}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), StateFileName+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// Observe records the file's current size and mtime and reports whether it is
// stable enough to process. A changed file is treated as a brand new version:
// any previous terminal result is cleared, so a failed download that is later
// replaced is retried while an unchanged failure is not.
func (s *State) Observe(path string, cur SizeMtime) Decision {
	fs, seen := s.Files[path]
	d := Decide(fs.Observed, seen, cur)
	if d == DecisionWriting {
		fs.Result = ""
		fs.Time = time.Time{}
		fs.Output = ""
		fs.Error = ""
	}
	fs.Observed = cur
	s.Files[path] = fs
	return d
}

// Settled reports whether path already reached a terminal result for its
// current observation, so it must not be processed again.
func (s *State) Settled(path string) bool {
	r := s.Files[path].Result
	return r == ResultDone || r == ResultFailed
}

// Record stores a terminal outcome for path together with the observation it
// applies to.
func (s *State) Record(path string, cur SizeMtime, result Result, at time.Time, output, errMsg string) {
	s.Files[path] = FileState{
		Observed: cur,
		Result:   result,
		Time:     at,
		Output:   output,
		Error:    errMsg,
	}
}
