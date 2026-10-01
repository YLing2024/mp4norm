// Package backup implements the on-disk backup store behind in-place
// normalization. Every write to a user's media file is preceded by an atomic
// rename of the original into this store, and every restore first backs up the
// version it is about to overwrite, so both directions are undoable.
//
// The store lives in a directory (by default ".mp4norm-backup" beside the
// media) holding the original bytes plus one small JSON sidecar per backup. The
// sidecar records where the backup came from so `backups`, `restore` and
// `forget` can find it without guessing.
package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DirName is the conventional backup directory created beside media files.
const DirName = ".mp4norm-backup"

// metaExt is appended to a backup file's name to form its sidecar metadata
// path. It is deliberately distinctive so it is never mistaken for media.
const metaExt = ".mp4norm.json"

// Entry describes one stored backup.
type Entry struct {
	// Backup is the file name inside the backup directory.
	Backup string `json:"backup"`
	// Original is the absolute path the backup was taken from.
	Original string `json:"original"`
	// Size is the byte size of the backed-up file.
	Size int64 `json:"size"`
	// Created is when this backup was taken.
	Created time.Time `json:"created"`

	// OriginalExists and BackupExists are filled in when listing, never stored.
	OriginalExists bool `json:"-"`
	BackupExists   bool `json:"-"`
}

// mu serializes store mutations. Batch normalization backs files up from many
// goroutines in one process, and the unique-name scan plus rename must not race.
var mu sync.Mutex

// DefaultDir returns the default backup directory for a media file.
func DefaultDir(file string) string {
	return filepath.Join(filepath.Dir(file), DirName)
}

// ResolveDir interprets a user-supplied directory for listing. A path that is
// itself a backup directory (named DirName or containing sidecars) is used
// as-is; otherwise the conventional child directory is used.
func ResolveDir(arg string) string {
	if filepath.Base(filepath.Clean(arg)) == DirName || hasSidecar(arg) {
		return arg
	}
	return filepath.Join(arg, DirName)
}

func hasSidecar(dir string) bool {
	des, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, de := range des {
		if strings.HasSuffix(de.Name(), metaExt) {
			return true
		}
	}
	return false
}

func metaPath(dir, backupName string) string {
	return filepath.Join(dir, backupName+metaExt)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// uniqueName picks a backup file name that collides with neither an existing
// backup nor a sidecar (including one whose backup file is currently missing).
// The first collision becomes ".1", then ".2", and so on, so the earliest
// original is never overwritten.
func uniqueName(dir, base string) string {
	name := base
	for i := 1; exists(filepath.Join(dir, name)) || exists(metaPath(dir, name)); i++ {
		name = fmt.Sprintf("%s.%d", base, i)
	}
	return name
}

// Move atomically renames src into dir and records a sidecar. It refuses to
// overwrite an existing backup. On a metadata failure the rename is rolled back
// so the original never ends up in an unmanaged location.
func Move(src, dir string) (Entry, error) {
	mu.Lock()
	defer mu.Unlock()
	return moveLocked(src, dir)
}

func moveLocked(src, dir string) (Entry, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Entry{}, err
	}
	abs, err := filepath.Abs(src)
	if err != nil {
		return Entry{}, err
	}
	fi, err := os.Stat(src)
	if err != nil {
		return Entry{}, err
	}
	name := uniqueName(dir, filepath.Base(src))
	dst := filepath.Join(dir, name)
	if err := os.Rename(src, dst); err != nil {
		return Entry{}, fmt.Errorf("move %s into backup: %w", src, err)
	}
	e := Entry{Backup: name, Original: abs, Size: fi.Size(), Created: time.Now()}
	if err := writeMeta(dir, e); err != nil {
		_ = os.Rename(dst, src)
		return Entry{}, err
	}
	return e, nil
}

func writeMeta(dir string, e Entry) error {
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".mp4norm-meta-*")
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
	if err := os.Rename(tmpName, metaPath(dir, e.Backup)); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// List returns every readable backup record in dir, newest first. OriginalExists
// and BackupExists are filled in so callers can show the current state.
func List(dir string) ([]Entry, error) {
	mu.Lock()
	defer mu.Unlock()
	return listLocked(dir)
}

func listLocked(dir string) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []Entry
	for _, de := range des {
		if de.IsDir() || !strings.HasSuffix(de.Name(), metaExt) {
			continue
		}
		e, err := readMeta(filepath.Join(dir, de.Name()))
		if err != nil {
			continue // a stray/corrupt sidecar must not hide the rest
		}
		e.OriginalExists = exists(e.Original)
		e.BackupExists = exists(filepath.Join(dir, e.Backup))
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if !entries[i].Created.Equal(entries[j].Created) {
			return entries[i].Created.After(entries[j].Created)
		}
		// Same clock tick: the later-suffixed name is the newer backup.
		return entries[i].Backup > entries[j].Backup
	})
	return entries, nil
}

func readMeta(path string) (Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return Entry{}, err
	}
	if e.Backup == "" || e.Original == "" {
		return Entry{}, fmt.Errorf("incomplete metadata in %s", path)
	}
	return e, nil
}

// Find returns the backups whose original path matches, newest first.
func Find(dir, original string) ([]Entry, error) {
	mu.Lock()
	defer mu.Unlock()
	return findLocked(dir, original)
}

func findLocked(dir, original string) ([]Entry, error) {
	abs, err := filepath.Abs(original)
	if err != nil {
		return nil, err
	}
	all, err := listLocked(dir)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range all {
		if filepath.Clean(e.Original) == filepath.Clean(abs) {
			out = append(out, e)
		}
	}
	return out, nil
}

// RestoreResult reports the outcome of a restore.
type RestoreResult struct {
	// Restored is the backup that was put back at the original path.
	Restored Entry
	// Replaced is the backup taken of the version that was in the way, if any.
	// It is non-nil when the current file existed and was preserved first.
	Replaced *Entry
}

// Restore puts the newest backup of original back in place. If a file still
// exists at original, it is moved into the store first, so the restore itself
// can be undone.
func Restore(dir, original string) (RestoreResult, error) {
	mu.Lock()
	defer mu.Unlock()

	entries, err := findLocked(dir, original)
	if err != nil {
		return RestoreResult{}, err
	}
	if len(entries) == 0 {
		return RestoreResult{}, fmt.Errorf("no backup found for %s", original)
	}
	newest := entries[0]
	backupPath := filepath.Join(dir, newest.Backup)
	if !exists(backupPath) {
		return RestoreResult{}, fmt.Errorf("backup file %s is missing", backupPath)
	}

	var replaced *Entry
	if exists(original) {
		e, err := moveLocked(original, dir)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("preserve current version before restore: %w", err)
		}
		replaced = &e
	}

	if err := os.Rename(backupPath, original); err != nil {
		return RestoreResult{}, fmt.Errorf("restore %s: %w", original, err)
	}
	// The restored backup is no longer stored; drop its sidecar.
	os.Remove(metaPath(dir, newest.Backup))
	newest.BackupExists = false
	newest.OriginalExists = true
	return RestoreResult{Restored: newest, Replaced: replaced}, nil
}

// Forget deletes every backup of original and returns how many were removed.
func Forget(dir, original string) (int, error) {
	mu.Lock()
	defer mu.Unlock()

	entries, err := findLocked(dir, original)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		os.Remove(filepath.Join(dir, e.Backup))
		os.Remove(metaPath(dir, e.Backup))
		n++
	}
	return n, nil
}
