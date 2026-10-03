package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"

	"github.com/YLing2024/mp4norm/internal/backup"
	"github.com/YLing2024/mp4norm/internal/probe"
)

// writeFixFixture writes a minimal progressive MP4 whose moov sits at the end
// of the file, so `fix` has real container work to do on it.
func writeFixFixture(t *testing.T, path string) {
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
	f.Children = []mp4.Box{ftyp, mdat, moov} // moov at the end

	var buf bytes.Buffer
	if err := f.Encode(&buf); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("expected %s not to exist", path)
	}
}

func assertMoovFront(t *testing.T, path string) {
	t.Helper()
	rep, err := probe.Analyze(path)
	if err != nil {
		t.Fatalf("probe %s: %v", path, err)
	}
	if rep.MoovPosition != probe.MoovFront {
		t.Fatalf("%s moov position = %s, want front", path, rep.MoovPosition)
	}
}

func TestRunFixSingleFile(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "clip.mp4")
	writeFixFixture(t, in)

	if err := runFix([]string{in}); err != nil {
		t.Fatalf("runFix single file: %v", err)
	}

	out := filepath.Join(dir, "clip.norm.mp4")
	assertExists(t, out)
	assertMoovFront(t, out)
	// The input must be left alone by default.
	rep, err := probe.Analyze(in)
	if err != nil {
		t.Fatalf("probe input: %v", err)
	}
	if rep.MoovPosition != probe.MoovEnd {
		t.Fatalf("input moov position = %s, want it untouched (end)", rep.MoovPosition)
	}
}

func TestRunFixDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFixFixture(t, filepath.Join(dir, "a.mp4"))
	writeFixFixture(t, filepath.Join(dir, "b.mp4"))

	if err := runFix([]string{dir}); err != nil {
		t.Fatalf("runFix directory: %v", err)
	}

	for _, name := range []string{"a.norm.mp4", "b.norm.mp4"} {
		out := filepath.Join(dir, name)
		assertExists(t, out)
		assertMoovFront(t, out)
	}
}

func TestRunFixMixedFileAndDirectory(t *testing.T) {
	base := t.TempDir()
	one := filepath.Join(base, "one.mp4")
	writeFixFixture(t, one)

	sub := filepath.Join(base, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	two := filepath.Join(sub, "two.mp4")
	writeFixFixture(t, two)

	if err := runFix([]string{one, sub}); err != nil {
		t.Fatalf("runFix mixed inputs: %v", err)
	}

	assertExists(t, filepath.Join(base, "one.norm.mp4"))
	assertExists(t, filepath.Join(sub, "two.norm.mp4"))
}

func TestRunFixInPlace(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "clip.mp4")
	writeFixFixture(t, in)

	if err := runFix([]string{"-in-place", in}); err != nil {
		t.Fatalf("runFix -in-place: %v", err)
	}

	// The original path now holds the fixed file, and a backup was kept.
	assertMoovFront(t, in)
	entries, err := os.ReadDir(backup.DefaultDir(in))
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no backup kept in %s", backup.DefaultDir(in))
	}
}

func TestRunFixOutdir(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFixFixture(t, filepath.Join(src, "a.mp4"))
	out := filepath.Join(base, "out")

	if err := runFix([]string{"-outdir", out, src}); err != nil {
		t.Fatalf("runFix -outdir: %v", err)
	}

	assertExists(t, filepath.Join(out, "a.norm.mp4"))
	assertMissing(t, filepath.Join(src, "a.norm.mp4"))
}

// TestRunHiddenAliases locks the compatibility contract: normalize and batch
// behave like fix, and scan and check are the same command.
func TestRunHiddenAliases(t *testing.T) {
	base := t.TempDir()
	single := filepath.Join(base, "single.mp4")
	writeFixFixture(t, single)

	if err := run([]string{"normalize", single}); err != nil {
		t.Fatalf("run normalize alias: %v", err)
	}
	assertExists(t, filepath.Join(base, "single.norm.mp4"))

	batchDir := filepath.Join(base, "batch")
	if err := os.MkdirAll(batchDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFixFixture(t, filepath.Join(batchDir, "b.mp4"))
	if err := run([]string{"batch", batchDir}); err != nil {
		t.Fatalf("run batch alias: %v", err)
	}
	assertExists(t, filepath.Join(batchDir, "b.norm.mp4"))

	// A moov-at-end fixture needs work, so both names exit with code 1.
	for _, name := range []string{"check", "scan"} {
		err := run([]string{name, single})
		var ec exitCodeError
		if !errors.As(err, &ec) || ec.code != 1 {
			t.Fatalf("run %s = %v, want exit code 1", name, err)
		}
	}
}
