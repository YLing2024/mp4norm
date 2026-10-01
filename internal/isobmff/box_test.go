package isobmff

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// mkBox builds a normal ISO-BMFF box.
func mkBox(typ string, payload []byte) []byte {
	n := 8 + len(payload)
	b := make([]byte, n)
	binary.BigEndian.PutUint32(b[0:4], uint32(n))
	copy(b[4:8], typ)
	copy(b[8:], payload)
	return b
}

// mkLargBox builds a 64-bit (largesize) box.
func mkLargBox(typ string, payload []byte) []byte {
	n := 16 + len(payload)
	b := make([]byte, n)
	binary.BigEndian.PutUint32(b[0:4], 1)
	copy(b[4:8], typ)
	binary.BigEndian.PutUint64(b[8:16], uint64(n))
	copy(b[16:], payload)
	return b
}

func TestScanTopLevel(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(mkBox("ftyp", []byte("isom\x00\x00\x02\x00isom")))
	buf.Write(mkBox("free", nil))
	buf.Write(mkBox("mdat", []byte("hello world")))
	mdatOff := buf.Len() - 8 - len("hello world")
	buf.Write(mkBox("moov", []byte("meta")))

	boxes, trunc, err := ScanTopLevel(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("ScanTopLevel: %v", err)
	}
	if trunc != nil {
		t.Fatalf("unexpected truncation: %+v", trunc)
	}
	want := []string{"ftyp", "free", "mdat", "moov"}
	if len(boxes) != len(want) {
		t.Fatalf("got %d boxes, want %d: %+v", len(boxes), len(want), boxes)
	}
	for i, w := range want {
		if boxes[i].Type != w {
			t.Errorf("box %d type = %q, want %q", i, boxes[i].Type, w)
		}
	}
	mdat := Find(boxes, "mdat")
	if mdat == nil || mdat.Offset != int64(mdatOff) {
		t.Errorf("mdat offset = %v, want %d", mdat, mdatOff)
	}
	if got := Count(boxes, "mdat"); got != 1 {
		t.Errorf("Count(mdat) = %d, want 1", got)
	}
}

func TestScanTopLevelLargSize(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(mkBox("ftyp", []byte("isom\x00\x00\x02\x00isom")))
	buf.Write(mkLargBox("mdat", []byte("data")))

	boxes, trunc, err := ScanTopLevel(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("ScanTopLevel: %v", err)
	}
	if trunc != nil {
		t.Fatalf("unexpected truncation: %+v", trunc)
	}
	mdat := Find(boxes, "mdat")
	if mdat == nil {
		t.Fatal("mdat not found")
	}
	if mdat.HeaderSize != largeHeaderSize {
		t.Errorf("HeaderSize = %d, want %d", mdat.HeaderSize, largeHeaderSize)
	}
	if want := int64(16 + 4); mdat.Size != want {
		t.Errorf("Size = %d, want %d", mdat.Size, want)
	}
}

func TestReadFtyp(t *testing.T) {
	payload := []byte("isom\x00\x00\x02\x00isommp42")
	raw := mkBox("ftyp", payload)
	major, compat, err := ReadFtyp(bytes.NewReader(raw), Box{Type: "ftyp", Offset: 0, Size: int64(len(raw)), HeaderSize: 8})
	if err != nil {
		t.Fatalf("ReadFtyp: %v", err)
	}
	if major != "isom" {
		t.Errorf("major = %q, want isom", major)
	}
	if len(compat) != 2 || compat[0] != "isom" || compat[1] != "mp42" {
		t.Errorf("compatible = %v, want [isom mp42]", compat)
	}
}

func TestScanTopLevelShortTail(t *testing.T) {
	// A normal box followed by fewer than 8 bytes of residue: the residue is
	// not enough for a header, so scanning stops successfully with the box.
	buf := append(mkBox("ftyp", []byte("isom\x00\x00\x02\x00isom")), 0, 0, 0)
	boxes, trunc, err := ScanTopLevel(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		t.Fatalf("ScanTopLevel: %v", err)
	}
	if trunc != nil {
		t.Fatalf("unexpected truncation on short tail: %+v", trunc)
	}
	if len(boxes) != 1 || boxes[0].Type != "ftyp" {
		t.Fatalf("boxes = %+v, want just ftyp", boxes)
	}
}

func TestScanTopLevelToleratesOverflow(t *testing.T) {
	// "not an mp4": the first four bytes are not a plausible box size, so the
	// declared box extends past EOF. This is tolerated by clamping the box to
	// the bytes present and recording the truncation, not by failing the scan.
	raw := []byte("not an mp4")
	boxes, trunc, err := ScanTopLevel(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("ScanTopLevel: %v", err)
	}
	if trunc == nil {
		t.Fatal("expected truncation to be recorded, got nil")
	}
	if trunc.Type != string(raw[4:8]) {
		t.Errorf("truncation type = %q, want %q", trunc.Type, string(raw[4:8]))
	}
	if trunc.Declared != int64(binary.BigEndian.Uint32(raw[0:4])) {
		t.Errorf("truncation declared = %d, want %d", trunc.Declared, binary.BigEndian.Uint32(raw[0:4]))
	}
	if trunc.Remaining != int64(len(raw)) {
		t.Errorf("truncation remaining = %d, want %d", trunc.Remaining, len(raw))
	}
	if len(boxes) != 1 {
		t.Fatalf("got %d boxes, want 1: %+v", len(boxes), boxes)
	}
	if boxes[0].Size != int64(len(raw)) {
		t.Errorf("clamped box size = %d, want %d", boxes[0].Size, len(raw))
	}
}

// Regression: a valid MP4 followed by trailing garbage must still scan.
func TestScanTopLevelTrailingGarbage(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(mkBox("ftyp", []byte("isom\x00\x00\x02\x00isom")))
	buf.Write(mkBox("mdat", []byte("video data")))
	buf.Write(mkBox("moov", []byte("meta")))
	realLen := int64(buf.Len())
	buf.WriteString("JUNKJUNKJUNK") // 12 trailing bytes

	boxes, trunc, err := ScanTopLevel(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("ScanTopLevel: %v", err)
	}
	if trunc == nil {
		t.Fatal("expected truncation for trailing garbage, got nil")
	}
	if trunc.Offset != realLen {
		t.Errorf("truncation offset = %d, want %d", trunc.Offset, realLen)
	}
	if want := []string{"ftyp", "mdat", "moov", "JUNK"}; !boxTypesEqual(boxes, want) {
		t.Errorf("box types = %v, want %v", boxTypes(boxes), want)
	}
	if got := boxes[len(boxes)-1].Size; got != trunc.Remaining {
		t.Errorf("clamped garbage size = %d, want %d", got, trunc.Remaining)
	}
}

// Regression: a valid MP4 followed by less than 8 bytes of residue must scan
// successfully, with no truncation reported.
func TestScanTopLevelTinyTail(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(mkBox("ftyp", []byte("isom\x00\x00\x02\x00isom")))
	buf.Write(mkBox("mdat", []byte("video data")))
	buf.Write(mkBox("moov", []byte("meta")))
	buf.WriteString("xy") // 2 trailing bytes

	boxes, trunc, err := ScanTopLevel(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("ScanTopLevel: %v", err)
	}
	if trunc != nil {
		t.Fatalf("unexpected truncation for tiny tail: %+v", trunc)
	}
	if want := []string{"ftyp", "mdat", "moov"}; !boxTypesEqual(boxes, want) {
		t.Errorf("box types = %v, want %v", boxTypes(boxes), want)
	}
}

func boxTypes(boxes []Box) []string {
	types := make([]string, len(boxes))
	for i, b := range boxes {
		types[i] = b.Type
	}
	return types
}

func boxTypesEqual(boxes []Box, want []string) bool {
	got := boxTypes(boxes)
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
