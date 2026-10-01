package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestMoveNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, DirName)
	src := filepath.Join(dir, "a.mp4")

	write(t, src, "first")
	e1, err := Move(src, store)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	write(t, src, "second")
	e2, err := Move(src, store)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if e1.Backup != "a.mp4" || e2.Backup != "a.mp4.1" {
		t.Fatalf("backup names = %q, %q, want a.mp4 and a.mp4.1", e1.Backup, e2.Backup)
	}
	if got := read(t, filepath.Join(store, e1.Backup)); got != "first" {
		t.Fatalf("first backup = %q, want the earliest original", got)
	}
	if got := read(t, filepath.Join(store, e2.Backup)); got != "second" {
		t.Fatalf("second backup = %q", got)
	}
}

func TestRestoreBacksUpCurrentVersion(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, DirName)
	original := filepath.Join(dir, "a.mp4")

	write(t, original, "original")
	if _, err := Move(original, store); err != nil {
		t.Fatalf("Move: %v", err)
	}
	write(t, original, "current")

	res, err := Restore(store, original)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := read(t, original); got != "original" {
		t.Fatalf("restored content = %q, want %q", got, "original")
	}
	if res.Replaced == nil {
		t.Fatal("Replaced = nil, want the current version preserved")
	}
	if got := read(t, filepath.Join(store, res.Replaced.Backup)); got != "current" {
		t.Fatalf("preserved current = %q, want %q", got, "current")
	}
	// The restored backup is consumed, so only the replaced copy remains.
	entries, err := Find(store, original)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(entries) != 1 || entries[0].Backup != res.Replaced.Backup {
		t.Fatalf("entries after restore = %+v, want only the replaced backup", entries)
	}
}

func TestRestoreMissingBackup(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, DirName)
	original := filepath.Join(dir, "a.mp4")
	write(t, original, "x")
	if _, err := Restore(store, original); err == nil {
		t.Fatal("Restore = nil, want a not-found error")
	}
}

func TestForgetRemovesEveryBackup(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, DirName)
	original := filepath.Join(dir, "a.mp4")

	write(t, original, "one")
	if _, err := Move(original, store); err != nil {
		t.Fatal(err)
	}
	write(t, original, "two")
	if _, err := Move(original, store); err != nil {
		t.Fatal(err)
	}
	n, err := Forget(store, original)
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if n != 2 {
		t.Fatalf("Forget removed %d, want 2", n)
	}
	entries, err := List(store)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries after forget = %+v, want none", entries)
	}
}

func TestResolveDir(t *testing.T) {
	media := t.TempDir()
	if got, want := ResolveDir(media), filepath.Join(media, DirName); got != want {
		t.Fatalf("ResolveDir(media) = %q, want %q", got, want)
	}
	if got := ResolveDir(filepath.Join(media, DirName)); got != filepath.Join(media, DirName) {
		t.Fatalf("ResolveDir(backup dir) = %q, want it unchanged", got)
	}
}
