// Package watch implements the polling loop behind the `watch` command: it
// scans a directory for files worth normalizing, waits until each file has
// stopped being written, and processes it exactly once. The stability rule and
// the persisted state machine live here, away from the CLI, so both can be
// unit tested without touching the filesystem or ffmpeg.
package watch

import "os"

// SizeMtime is the cheap identity of a file at one instant. Equality between
// two observations is the signal that a file has finished being written.
type SizeMtime struct {
	Size  int64 `json:"size"`
	Mtime int64 `json:"mtime"` // UnixNano
}

// Stat reads the current size and modification time of path.
func Stat(path string) (SizeMtime, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return SizeMtime{}, err
	}
	return SizeMtime{Size: fi.Size(), Mtime: fi.ModTime().UnixNano()}, nil
}

// Decision is the stability verdict for one file in one scan pass.
type Decision int

const (
	// DecisionStable means the file looked identical to the previous pass and
	// is safe to process.
	DecisionStable Decision = iota
	// DecisionFirstSeen means this pass is the first time the file was
	// observed; it is recorded and left alone until the next pass, so a
	// download that started just before watch did cannot slip through as if
	// it were finished.
	DecisionFirstSeen
	// DecisionWriting means the file changed since the previous pass; it is
	// still being written and must not be touched.
	DecisionWriting
)

// Decide compares the file's current size and mtime with the observation from
// the previous pass. seen says whether prev was ever recorded.
func Decide(prev SizeMtime, seen bool, cur SizeMtime) Decision {
	switch {
	case !seen:
		return DecisionFirstSeen
	case cur == prev:
		return DecisionStable
	default:
		return DecisionWriting
	}
}
