package safefile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"

	"github.com/YLing2024/mp4norm/internal/backup"
	"github.com/YLing2024/mp4norm/internal/normalize"
	"github.com/YLing2024/mp4norm/internal/probe"
)

// buildProgressive builds a minimal progressive MP4 with one video sample.
func buildProgressive(t *testing.T, moovAtEnd bool) []byte {
	t.Helper()
	const sampleSize = 4096

	ftyp := mp4.NewFtyp("isom", 0x200, []string{"isom", "mp42"})
	moov := mp4.NewMoovBox()
	moov.AddChild(mp4.CreateMvhd())

	trak := mp4.CreateEmptyTrak(1, 90000, "vide", "und")
	trak.Mdia.Mdhd.Duration = 90000 // 1 s
	stbl := trak.Mdia.Minf.Stbl
	stbl.Stts.SampleCount = []uint32{1}
	stbl.Stts.SampleTimeDelta = []uint32{3000}
	stbl.Stsc.Entries = []mp4.StscEntry{{FirstChunk: 1, SamplesPerChunk: 1}}
	stbl.Stsc.SampleDescriptionID = []uint32{1}
	stbl.Stsz.SampleNumber = 1
	stbl.Stsz.SampleSize = []uint32{sampleSize}
	stbl.Stco.ChunkOffset = []uint32{0}
	moov.AddChild(trak)
	moov.Trak = trak
	moov.Traks = []*mp4.TrakBox{trak}

	mdat := &mp4.MdatBox{}
	mdat.SetData(bytes.Repeat([]byte{0x5a}, sampleSize))

	f := mp4.NewFile()
	f.Ftyp = ftyp
	f.Moov = moov
	f.Mdat = mdat
	if moovAtEnd {
		f.Children = []mp4.Box{ftyp, mdat, moov}
	} else {
		f.Children = []mp4.Box{ftyp, moov, mdat}
	}
	var buf bytes.Buffer
	if err := f.Encode(&buf); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return buf.Bytes()
}

func sha256Of(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writeMedia(t *testing.T, moovAtEnd bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(path, buildProgressive(t, moovAtEnd), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func faststartTransform(src normalize.ReadSeekerAt, dst io.Writer) (*normalize.Result, error) {
	return normalize.Faststart(src, dst)
}

func TestTransformNewFileLeavesInputAlone(t *testing.T) {
	in := writeMedia(t, true)
	orig := sha256Of(t, in)
	out := filepath.Join(filepath.Dir(in), "out.mp4")

	oc, err := Transform(Request{Input: in, Output: out, Transform: faststartTransform})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if oc.Output != out {
		t.Fatalf("output = %q, want %q", oc.Output, out)
	}
	if got := sha256Of(t, in); got != orig {
		t.Fatal("input changed in new-file mode")
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output missing: %v", err)
	}
}

func TestTransformInPlaceBacksUpOriginal(t *testing.T) {
	in := writeMedia(t, true)
	orig := sha256Of(t, in)

	oc, err := Transform(Request{Input: in, InPlace: true, Transform: faststartTransform})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if oc.Backup == nil {
		t.Fatal("no backup entry recorded")
	}
	if got := sha256Of(t, oc.BackupPath); got != orig {
		t.Fatalf("backup sha = %s, want the original %s", got, orig)
	}
	if got := sha256Of(t, in); got == orig {
		t.Fatal("in-place output still equals the original")
	}
	if oc.After == nil || oc.After.MoovPosition != probe.MoovFront {
		t.Fatalf("after moov position = %+v, want front", oc.After)
	}
}

func TestInPlaceSecondBackupGetsSuffix(t *testing.T) {
	in := writeMedia(t, true)
	if _, err := Transform(Request{Input: in, InPlace: true, Transform: faststartTransform}); err != nil {
		t.Fatalf("first Transform: %v", err)
	}
	oc, err := Transform(Request{Input: in, InPlace: true, Transform: faststartTransform})
	if err != nil {
		t.Fatalf("second Transform: %v", err)
	}
	if oc.Backup.Backup != "clip.mp4.1" {
		t.Fatalf("second backup name = %q, want clip.mp4.1", oc.Backup.Backup)
	}
}

func TestInPlaceCustomBackupDir(t *testing.T) {
	in := writeMedia(t, true)
	store := filepath.Join(filepath.Dir(in), "custom-backups")

	oc, err := Transform(Request{Input: in, InPlace: true, BackupDir: store, Transform: faststartTransform})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if filepath.Dir(oc.BackupPath) != store {
		t.Fatalf("backup path = %q, want it inside %q", oc.BackupPath, store)
	}
}

// collectLogs returns a logger that appends formatted lines to a slice.
func collectLogs() (*[]string, func(format string, args ...any)) {
	var logs []string
	return &logs, func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}
}

// TestCleanStaleTempsRemovesOnlyOldTempFiles is the core of the residue fix:
// an old, killed-process temp file is swept and reported, while a fresh one
// (possibly a concurrently running instance) is left untouched, as is every
// file that is not a rewrite temp at all.
func TestCleanStaleTempsRemovesOnlyOldTempFiles(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, TempPrefix+"stale")
	fresh := filepath.Join(dir, TempPrefix+"fresh")
	other := filepath.Join(dir, "keep.txt")
	for _, p := range []string{stale, fresh, other} {
		if err := os.WriteFile(p, []byte("payload"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	logs, logf := collectLogs()
	removed := CleanStaleTemps(dir, StaleTempAfter, logf)

	if len(removed) != 1 || removed[0].Path != stale {
		t.Fatalf("removed = %+v, want only %s", removed, stale)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale temp still present: %v", err)
	}
	for _, p := range []string{fresh, other} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s was removed, want it kept: %v", p, err)
		}
	}
	if len(*logs) != 1 {
		t.Fatalf("logs = %v, want exactly one removal line", *logs)
	}
}

// TestTransformSweepsStaleTempResidue runs a real rewrite with old residue in
// the target directory and requires the residue to be gone and logged.
func TestTransformSweepsStaleTempResidue(t *testing.T) {
	in := writeMedia(t, true)
	dir := filepath.Dir(in)
	stale := filepath.Join(dir, TempPrefix+"residue")
	if err := os.WriteFile(stale, []byte("residue"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	logs, logf := collectLogs()
	out := filepath.Join(dir, "out.mp4")
	if _, err := Transform(Request{Input: in, Output: out, Transform: faststartTransform, Logf: logf}); err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale residue survived the rewrite: %v", err)
	}
	if len(*logs) == 0 {
		t.Fatal("cleanup was silent, want a log line")
	}
}

// TestTransformKeepsFreshTempFile guards concurrent instances: a temp file with
// a current mtime must never be swept by another run.
func TestTransformKeepsFreshTempFile(t *testing.T) {
	in := writeMedia(t, true)
	dir := filepath.Dir(in)
	fresh := filepath.Join(dir, TempPrefix+"active")
	if err := os.WriteFile(fresh, []byte("active"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "out.mp4")
	if _, err := Transform(Request{Input: in, Output: out, Transform: faststartTransform}); err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh temp (concurrent instance) was removed: %v", err)
	}
}

// TestInPlaceTruncatedProductKeepsOriginal is the failure drill: the transform
// produces a valid-but-truncated file. Verification must reject it, delete the
// product, leave no backup, and leave the source byte-for-byte identical.
func TestInPlaceTruncatedProductKeepsOriginal(t *testing.T) {
	in := writeMedia(t, true)
	orig := sha256Of(t, in)
	t.Logf("source sha256 before      = %s", orig)

	truncating := func(src normalize.ReadSeekerAt, dst io.Writer) (*normalize.Result, error) {
		res, err := normalize.Faststart(src, dst)
		if err != nil {
			return nil, err
		}
		// Simulate a write that lost its tail: the mdat header keeps claiming
		// more bytes than the file now holds.
		if f, ok := dst.(*os.File); ok {
			fi, statErr := f.Stat()
			if statErr != nil {
				return nil, statErr
			}
			if err := f.Truncate(fi.Size() - 16); err != nil {
				return nil, err
			}
			t.Logf("deliberately truncated the product from %d to %d bytes", fi.Size(), fi.Size()-16)
		}
		return res, nil
	}

	_, err := Transform(Request{Input: in, InPlace: true, Transform: truncating})
	t.Logf("Transform error           = %v", err)
	if err == nil {
		t.Fatal("Transform = nil, want a self-check failure")
	}
	after := sha256Of(t, in)
	t.Logf("source sha256 after       = %s", after)
	if after != orig {
		t.Fatalf("source sha = %s, want it untouched at %s", after, orig)
	}
	entries, listErr := backup.List(backup.DefaultDir(in))
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	t.Logf("backups left behind       = %d", len(entries))
	if len(entries) != 0 {
		t.Fatalf("backups = %+v, want none after a failed check", entries)
	}
	// No stray temp file may be left behind.
	des, err := os.ReadDir(filepath.Dir(in))
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range des {
		if strings.HasPrefix(de.Name(), TempPrefix) {
			t.Fatalf("temp file left behind: %s", de.Name())
		}
	}
}
