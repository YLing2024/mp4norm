package normalize

import (
	"bytes"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
)

const mediaSize = 4 // one sample of 4 bytes

// buildProgressive builds a minimal, valid progressive MP4 with one sample and
// returns its bytes. When moovAtEnd is true the mdat precedes the moov.
func buildProgressive(t *testing.T, moovAtEnd bool) []byte {
	t.Helper()

	ftyp := mp4.NewFtyp("isom", 0x200, []string{"isom", "mp42"})

	moov := mp4.NewMoovBox()
	moov.AddChild(mp4.CreateMvhd())

	trak := mp4.CreateEmptyTrak(1, 90000, "vide", "und")
	stbl := trak.Mdia.Minf.Stbl
	// Mutate the boxes created by CreateEmptyTrak in place: stbl encodes its
	// Children slice, so assigning new pointers would have no effect.
	// A non-empty stts is required for mp4ff to treat the file as progressive.
	stbl.Stts.SampleCount = []uint32{1}
	stbl.Stts.SampleTimeDelta = []uint32{3000}
	stbl.Stsc.Entries = []mp4.StscEntry{{FirstChunk: 1, SamplesPerChunk: 1}}
	stbl.Stsc.SampleDescriptionID = []uint32{1}
	stbl.Stsz.SampleNumber = 1
	stbl.Stsz.SampleSize = []uint32{mediaSize}
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

func firstChunkOffset(t *testing.T, data []byte) (uint32, uint64) {
	t.Helper()
	f, err := mp4.DecodeFile(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Moov == nil || len(f.Moov.Traks) == 0 {
		t.Fatal("no traks decoded")
	}
	return f.Moov.Traks[0].Mdia.Minf.Stbl.Stco.ChunkOffset[0], f.Moov.Size()
}

func TestFaststartMovesMoovToFront(t *testing.T) {
	raw := buildProgressive(t, true)
	oldOffset, moovSize := firstChunkOffset(t, raw)

	var out bytes.Buffer
	res, err := Faststart(bytes.NewReader(raw), &out)
	if err != nil {
		t.Fatalf("Faststart: %v", err)
	}
	if !res.Changed {
		t.Error("Changed = false, want true")
	}
	if res.Delta != int64(moovSize) {
		t.Errorf("Delta = %d, want moov size %d", res.Delta, moovSize)
	}

	f, err := mp4.DecodeFile(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	gotTypes := make([]string, 0, len(f.Children))
	for _, b := range f.Children {
		gotTypes = append(gotTypes, b.Type())
	}
	want := []string{"ftyp", "moov", "mdat"}
	if len(gotTypes) != len(want) {
		t.Fatalf("output boxes = %v, want %v", gotTypes, want)
	}
	for i := range want {
		if gotTypes[i] != want[i] {
			t.Fatalf("output boxes = %v, want %v", gotTypes, want)
		}
	}

	newOffset := f.Moov.Traks[0].Mdia.Minf.Stbl.Stco.ChunkOffset[0]
	if newOffset != oldOffset+uint32(moovSize) { //nolint:gosec // test values are small
		t.Errorf("chunk offset = %d, want %d", newOffset, oldOffset+uint32(moovSize))
	}
}

func TestFaststartAlreadyNormalized(t *testing.T) {
	raw := buildProgressive(t, false)
	var out bytes.Buffer
	res, err := Faststart(bytes.NewReader(raw), &out)
	if err != nil {
		t.Fatalf("Faststart: %v", err)
	}
	if res.Changed {
		t.Errorf("Changed = true, want false (already faststart)")
	}
	if res.Delta != 0 {
		t.Errorf("Delta = %d, want 0", res.Delta)
	}
	if !bytes.Equal(raw, out.Bytes()) {
		t.Error("output differs from input for an already-normalized file")
	}
}
