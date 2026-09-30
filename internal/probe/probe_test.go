package probe

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

func writeMP4(t *testing.T, parts ...[]byte) string {
	t.Helper()
	var buf []byte
	for _, p := range parts {
		buf = append(buf, p...)
	}
	path := filepath.Join(t.TempDir(), "test.mp4")
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func ftypBox() []byte {
	return mkBox("ftyp", []byte("isom\x00\x00\x02\x00isommp42"))
}

func hasCode(rep *Report, code string) bool {
	for _, f := range rep.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestAnalyzeMoovAtEnd(t *testing.T) {
	path := writeMP4(t, ftypBox(), mkBox("mdat", []byte("video data")), mkBox("moov", []byte("meta")))
	rep, err := Analyze(path)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if rep.MoovPosition != MoovEnd {
		t.Errorf("MoovPosition = %q, want %q", rep.MoovPosition, MoovEnd)
	}
	if rep.MajorBrand != "isom" {
		t.Errorf("MajorBrand = %q, want isom", rep.MajorBrand)
	}
	if !hasCode(rep, "moov-at-end") {
		t.Errorf("expected moov-at-end finding, got %+v", rep.Findings)
	}
	if !rep.NeedsNormalization() {
		t.Error("NeedsNormalization = false, want true")
	}
}

func TestAnalyzeFaststart(t *testing.T) {
	path := writeMP4(t, ftypBox(), mkBox("moov", []byte("meta")), mkBox("mdat", []byte("video data")))
	rep, err := Analyze(path)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if rep.MoovPosition != MoovFront {
		t.Errorf("MoovPosition = %q, want %q", rep.MoovPosition, MoovFront)
	}
	if rep.NeedsNormalization() {
		t.Error("NeedsNormalization = true, want false")
	}
	if hasCode(rep, "moov-at-end") {
		t.Error("did not expect moov-at-end finding")
	}
}

func TestAnalyzeFragmented(t *testing.T) {
	path := writeMP4(t, ftypBox(), mkBox("moov", []byte("meta")),
		mkBox("moof", []byte("mfhd")), mkBox("mdat", []byte("video data")))
	rep, err := Analyze(path)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !rep.Fragmented {
		t.Error("Fragmented = false, want true")
	}
	if !hasCode(rep, "fragmented") {
		t.Errorf("expected fragmented finding, got %+v", rep.Findings)
	}
	if !rep.NeedsNormalization() {
		t.Error("NeedsNormalization = false, want true")
	}
}

func TestAnalyzeMissingMoov(t *testing.T) {
	path := writeMP4(t, ftypBox(), mkBox("mdat", []byte("video data")))
	rep, err := Analyze(path)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if rep.MoovPosition != MoovUnknown {
		t.Errorf("MoovPosition = %q, want %q", rep.MoovPosition, MoovUnknown)
	}
	if !hasCode(rep, "moov-missing") {
		t.Errorf("expected moov-missing finding, got %+v", rep.Findings)
	}
}
