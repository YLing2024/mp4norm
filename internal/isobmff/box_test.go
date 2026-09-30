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

	boxes, err := ScanTopLevel(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("ScanTopLevel: %v", err)
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

	boxes, err := ScanTopLevel(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("ScanTopLevel: %v", err)
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

func TestScanTopLevelTruncated(t *testing.T) {
	raw := []byte{0, 0, 0} // fewer than 8 bytes
	if _, err := ScanTopLevel(bytes.NewReader(raw), int64(len(raw))); err == nil {
		t.Fatal("expected error for truncated header, got nil")
	}
}
