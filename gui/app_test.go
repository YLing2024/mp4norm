package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"

	"github.com/YLing2024/mp4norm/internal/safefile"
)

func mkBox(typ string, payload []byte) []byte {
	n := 8 + len(payload)
	b := make([]byte, n)
	binary.BigEndian.PutUint32(b[0:4], uint32(n))
	copy(b[4:8], typ)
	copy(b[8:], payload)
	return b
}

func ftypBox() []byte { return mkBox("ftyp", []byte("isom\x00\x00\x02\x00isommp42")) }

func writeFile(t *testing.T, path string, parts ...[]byte) {
	t.Helper()
	var buf []byte
	for _, p := range parts {
		buf = append(buf, p...)
	}
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// buildProgressive builds a minimal, valid progressive MP4 with one video
// sample. When moovAtEnd is true the mdat precedes the moov, so the file needs
// (and is a candidate for) normalization.
func buildProgressive(t *testing.T, moovAtEnd bool) []byte {
	t.Helper()

	ftyp := mp4.NewFtyp("isom", 0x200, []string{"isom", "mp42"})
	moov := mp4.NewMoovBox()
	moov.AddChild(mp4.CreateMvhd())

	trak := mp4.CreateEmptyTrak(1, 90000, "vide", "und")
	stbl := trak.Mdia.Minf.Stbl
	stbl.Stts.SampleCount = []uint32{1}
	stbl.Stts.SampleTimeDelta = []uint32{3000}
	stbl.Stsc.Entries = []mp4.StscEntry{{FirstChunk: 1, SamplesPerChunk: 1}}
	stbl.Stsc.SampleDescriptionID = []uint32{1}
	stbl.Stsz.SampleNumber = 1
	stbl.Stsz.SampleSize = []uint32{4}
	stbl.Stco.ChunkOffset = []uint32{0}
	moov.AddChild(trak)
	moov.Trak = trak
	moov.Traks = []*mp4.TrakBox{trak}

	mdat := &mp4.MdatBox{}
	mdat.SetData([]byte("abcd"))

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
		t.Fatalf("encode synthetic file: %v", err)
	}
	return buf.Bytes()
}

func sha256Of(t *testing.T, path string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return sha256.Sum256(b)
}

func TestBuildSpecValidatesOutputMode(t *testing.T) {
	if _, err := buildSpec("normalize", "in.mp4", "out.mp4", ".norm.mp4", true, ""); err == nil {
		t.Fatal("in-place with an output path: want an error")
	}
	if _, err := buildSpec("normalize", "in.mp4", "", ".norm.mp4", false, "bk"); err == nil {
		t.Fatal("backup dir without in-place: want an error")
	}
	spec, err := buildSpec("normalize", "in.mp4", "", ".norm.mp4", false, "")
	if err != nil {
		t.Fatalf("buildSpec: %v", err)
	}
	if spec.inPlace || spec.output != "in.norm.mp4" {
		t.Fatalf("spec = %+v, want new-file output in.norm.mp4", spec)
	}
}

func TestNormalizeInPlaceCreatesBackupWithGain(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "clip.mp4")
	writeFile(t, in, buildProgressive(t, true))
	original := sha256Of(t, in)

	app := NewApp()
	res, err := app.Normalize(NormalizeRequest{Input: in, Format: "progressive", WindowMs: 1000, InPlace: true})
	if err != nil {
		t.Fatalf("Normalize in-place: %v", err)
	}
	if !res.InPlace || res.BackupPath == "" {
		t.Fatalf("result = %+v, want an in-place run with a backup path", res)
	}
	if res.Gain == nil || res.Gain.BeforeFirstPlay == 0 {
		t.Fatalf("gain = %+v, want before/after numbers", res.Gain)
	}

	// The original is now at the backup path, byte-for-byte.
	if got := sha256Of(t, res.BackupPath); got != original {
		t.Fatal("backup does not match the original bytes")
	}
	// The published file leads with moov (faststart), so it differs from source.
	if after := sha256Of(t, in); after == original {
		t.Fatal("input was not replaced")
	}
	if rep, err := app.Probe(in); err != nil || rep.MoovPosition != "front" {
		t.Fatalf("normalized moov position = %v (err %v), want front", rep, err)
	}
}

func TestNormalizeNewFileLeavesInputUntouched(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "clip.mp4")
	writeFile(t, in, buildProgressive(t, true))
	original := sha256Of(t, in)

	app := NewApp()
	res, err := app.Normalize(NormalizeRequest{Input: in, Format: "progressive", WindowMs: 1000})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if res.InPlace || res.BackupPath != "" {
		t.Fatalf("result = %+v, want a plain new-file run", res)
	}
	if res.Output != filepath.Join(dir, "clip.norm.mp4") {
		t.Fatalf("output = %q, want clip.norm.mp4 beside the input", res.Output)
	}
	if got := sha256Of(t, in); got != original {
		t.Fatal("input changed in new-file mode")
	}
	if _, err := os.Stat(res.Output); err != nil {
		t.Fatalf("output missing: %v", err)
	}
}

// TestNormalizeSweepsAndClearsTempTracking checks the GUI rewrite path end to
// end: stale residue is swept before the rewrite, the in-flight tracking is
// cleared afterwards, and no temp file of any kind is left behind.
func TestNormalizeSweepsAndClearsTempTracking(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "clip.mp4")
	writeFile(t, in, buildProgressive(t, true))

	stale := filepath.Join(dir, safefile.TempPrefix+"residue")
	if err := os.WriteFile(stale, []byte("residue"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	if _, err := app.Normalize(NormalizeRequest{Input: in, Format: "progressive", WindowMs: 1000}); err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale residue survived the rewrite: %v", err)
	}
	app.tmpMu.Lock()
	tracked := app.tmpPath
	app.tmpMu.Unlock()
	if tracked != "" {
		t.Fatalf("temp tracking not cleared after Normalize: %q", tracked)
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range des {
		if strings.HasPrefix(de.Name(), safefile.TempPrefix) {
			t.Fatalf("temp file left behind: %s", de.Name())
		}
	}
}

// TestShutdownRemovesTrackedTempFile covers the graceful-close path: a temp
// file of a rewrite still in flight is deleted and the tracking is reset.
func TestShutdownRemovesTrackedTempFile(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, safefile.TempPrefix+"inflight")
	if err := os.WriteFile(tmp, []byte("residue"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.trackTemp(tmp)
	app.shutdown(context.Background())

	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("tracked temp survived shutdown: %v", err)
	}
	if p := app.tmpPath; p != "" {
		t.Fatalf("tmpPath = %q after shutdown, want empty", p)
	}
}

func TestBatchNormalizeInPlaceAggregatesGain(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.mp4")
	b := filepath.Join(dir, "b.mp4")
	writeFile(t, a, buildProgressive(t, true))
	writeFile(t, b, buildProgressive(t, true))

	app := NewApp()
	res, err := app.BatchNormalize(BatchRequest{
		Inputs: []string{a, b}, Format: "progressive", WindowMs: 1000, InPlace: true,
	})
	if err != nil {
		t.Fatalf("BatchNormalize: %v", err)
	}
	if res.Done != 2 || res.Failed != 0 {
		t.Fatalf("result = %+v, want 2 done / 0 failed", res)
	}
	if res.BeforeFirstPlay == 0 || res.AfterFirstPlay == 0 {
		t.Fatalf("aggregate gain = (%d -> %d), want both non-zero", res.BeforeFirstPlay, res.AfterFirstPlay)
	}
	for _, o := range res.Results {
		if o.Gain == nil || o.BackupPath == "" {
			t.Fatalf("outcome = %+v, want gain and backup path", o)
		}
	}
}

func TestBackupManagementListRestoreDelete(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "clip.mp4")
	writeFile(t, in, buildProgressive(t, true))
	original := sha256Of(t, in)

	app := NewApp()
	if _, err := app.Normalize(NormalizeRequest{Input: in, Format: "progressive", WindowMs: 1000, InPlace: true}); err != nil {
		t.Fatalf("in-place normalize: %v", err)
	}

	list, err := app.ListBackups(dir)
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(list.Entries) != 1 || list.Entries[0].Original != in || !list.Entries[0].OriginalExists {
		t.Fatalf("entries = %+v, want one entry for %s", list.Entries, in)
	}

	out, err := app.RestoreBackup(dir, in)
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if out.Restored.Backup == "" || out.ReplacedBackup == "" {
		t.Fatalf("restore outcome = %+v, want both the restored and replaced backups", out)
	}
	if got := sha256Of(t, in); got != original {
		t.Fatal("restore did not bring the original bytes back")
	}

	if n, err := app.DeleteBackups(dir, in); err != nil || n != 1 {
		t.Fatalf("DeleteBackups = (%d, %v), want (1, nil)", n, err)
	}
	list, err = app.ListBackups(dir)
	if err != nil {
		t.Fatalf("ListBackups after delete: %v", err)
	}
	if len(list.Entries) != 0 {
		t.Fatalf("entries after delete = %+v, want none", list.Entries)
	}
}

func TestScanPathsClassifiesDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ok.mp4"), ftypBox(), mkBox("moov", []byte("meta")), mkBox("mdat", []byte("data")))
	writeFile(t, filepath.Join(dir, "work.mp4"), ftypBox(), mkBox("mdat", []byte("data")), mkBox("moov", []byte("meta")))
	if err := os.WriteFile(filepath.Join(dir, "bad.mp4"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	verdicts, err := app.ScanPaths([]string{dir})
	if err != nil {
		t.Fatalf("ScanPaths: %v", err)
	}
	got := make(map[string]string, len(verdicts))
	for _, v := range verdicts {
		got[v.Name] = string(v.Status)
	}
	want := map[string]string{"ok.mp4": "ok", "work.mp4": "needs_work", "bad.mp4": "broken"}
	for name, status := range want {
		if got[name] != status {
			t.Errorf("%s status = %q, want %q", name, got[name], status)
		}
	}
}

func TestBatchNormalizeReportsFailures(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.mp4")
	if err := os.WriteFile(bad, []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	res, err := app.BatchNormalize(BatchRequest{Inputs: []string{bad}, Format: "progressive", WindowMs: 1000})
	if err != nil {
		t.Fatalf("BatchNormalize: %v", err)
	}
	if res.Total != 1 || res.Failed != 1 || res.Done != 0 {
		t.Fatalf("result = %+v, want total=1 failed=1 done=0", res)
	}
	if len(res.Results) != 1 || res.Results[0].Status != "failed" {
		t.Fatalf("results = %+v, want one failed outcome", res.Results)
	}
}

func TestParseTargetsSkipsWailsFlags(t *testing.T) {
	got := parseTargets([]string{
		"-loglevel", "debug",
		`D:\videos`,
		"--assetdir=frontend/dist",
		"-devserver", "localhost:34115",
		"clip.mp4",
	})
	want := []string{`D:\videos`, "clip.mp4"}
	if len(got) != len(want) {
		t.Fatalf("parseTargets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseTargets = %v, want %v", got, want)
		}
	}
}

func TestParseTargetsEmptyWithoutArguments(t *testing.T) {
	if got := parseTargets(nil); len(got) != 0 {
		t.Fatalf("parseTargets(nil) = %v, want empty", got)
	}
}

func TestInitialTargetsExposed(t *testing.T) {
	if got := NewApp().InitialTargets(); got == nil || len(got) != 0 {
		t.Fatalf("NewApp().InitialTargets() = %v, want empty non-nil slice", got)
	}
	got := NewApp(`D:\videos`, "clip.mp4").InitialTargets()
	want := []string{`D:\videos`, "clip.mp4"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("InitialTargets() = %v, want %v", got, want)
	}
}
