package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
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
