package probe

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
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

func hasBoxType(rep *Report, typ string) bool {
	for _, b := range rep.Boxes {
		if b.Type == typ {
			return true
		}
	}
	return false
}

// requireValidError asserts that err is the "not a valid MP4/ISO BMFF file"
// rejection, which must only fire for an empty/misplaced start.
func requireValidError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not a valid MP4/ISO BMFF file") {
		t.Fatalf("error = %v, want it to mention not a valid MP4/ISO BMFF file", err)
	}
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

func TestAnalyzeRejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake.mp4")
	if err := os.WriteFile(path, []byte("not an mp4"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	_, err := Analyze(path)
	requireValidError(t, err)
}

// Regression: a normal MP4 followed by trailing garbage bytes must still be
// analysed, with every real box present and a truncated-box warning attached.
func TestAnalyzeTrailingGarbage(t *testing.T) {
	path := writeMP4(t, ftypBox(), mkBox("mdat", []byte("video data")), mkBox("moov", []byte("meta")), []byte("JUNKJUNKJUNK"))
	rep, err := Analyze(path)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	for _, typ := range []string{"ftyp", "mdat", "moov"} {
		if !hasBoxType(rep, typ) {
			t.Errorf("missing %s box: %+v", typ, rep.Boxes)
		}
	}
	if !hasCode(rep, "truncated-box") {
		t.Errorf("expected truncated-box finding, got %+v", rep.Findings)
	}
}

// Regression: a normal MP4 followed by fewer than 8 bytes of residue must be
// analysed successfully, and must not be reported as truncated.
func TestAnalyzeTinyTail(t *testing.T) {
	path := writeMP4(t, ftypBox(), mkBox("mdat", []byte("video data")), mkBox("moov", []byte("meta")), []byte("xy"))
	rep, err := Analyze(path)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	for _, typ := range []string{"ftyp", "mdat", "moov"} {
		if !hasBoxType(rep, typ) {
			t.Errorf("missing %s box: %+v", typ, rep.Boxes)
		}
	}
	if hasCode(rep, "truncated-box") {
		t.Errorf("did not expect truncated-box finding, got %+v", rep.Findings)
	}
}

// Regression: a file whose first box is not ftyp/styp is rejected even though
// its top-level boxes parse cleanly.
func TestAnalyzeRejectsNoFtyp(t *testing.T) {
	path := writeMP4(t, mkBox("free", nil), mkBox("mdat", []byte("video data")))
	_, err := Analyze(path)
	requireValidError(t, err)
}

// Regression: zero top-level boxes (empty file) is the other rejection case.
func TestAnalyzeRejectsEmpty(t *testing.T) {
	path := writeMP4(t)
	_, err := Analyze(path)
	requireValidError(t, err)
}

// A clean MP4 must not carry the truncation warning.
func TestAnalyzeNoTruncationWarning(t *testing.T) {
	path := writeMP4(t, ftypBox(), mkBox("moov", []byte("meta")), mkBox("mdat", []byte("video data")))
	rep, err := Analyze(path)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if hasCode(rep, "truncated-box") {
		t.Errorf("unexpected truncated-box finding, got %+v", rep.Findings)
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
