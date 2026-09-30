package normalize

import (
	"bytes"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
)

func concatSamples(prefix byte, sizes []uint32) []byte {
	var out []byte
	for i, s := range sizes {
		b := make([]byte, s)
		for j := range b {
			b[j] = prefix + byte(i)
		}
		out = append(out, b...)
	}
	return out
}

func addSingleChunkTrack(moov *mp4.MoovBox, trackID, scale uint32, handler string, sizes []uint32, durEach, chunkOffset uint32) *mp4.TrakBox {
	trak := mp4.CreateEmptyTrak(trackID, scale, handler, "und")
	stbl := trak.Mdia.Minf.Stbl
	stbl.Stts.SampleCount = []uint32{uint32(len(sizes))} //nolint:gosec // small test values
	stbl.Stts.SampleTimeDelta = []uint32{durEach}
	stbl.Stsc.Entries = []mp4.StscEntry{{FirstChunk: 1, SamplesPerChunk: uint32(len(sizes)), FirstSampleNr: 1}} //nolint:gosec // small test values
	stbl.Stsc.SampleDescriptionID = []uint32{1}
	stbl.Stsz.SampleNumber = uint32(len(sizes)) //nolint:gosec // small test values
	stbl.Stsz.SampleSize = append([]uint32(nil), sizes...)
	stbl.Stco.ChunkOffset = []uint32{chunkOffset}
	moov.AddChild(trak)
	return trak
}

// buildInterleaveFixture builds a two-track progressive MP4 whose mdat stores
// all video samples followed by all audio samples (i.e. not interleaved).
func buildInterleaveFixture(t *testing.T, moovAtEnd bool) (data, videoBytes, audioBytes []byte) {
	t.Helper()

	videoSizes := []uint32{3, 4, 5, 6}
	audioSizes := []uint32{2, 2, 3, 2, 2, 2, 2, 3}

	videoBytes = concatSamples(0x10, videoSizes)
	audioBytes = concatSamples(0x60, audioSizes)

	ftyp := mp4.NewFtyp("isom", 0x200, []string{"isom", "mp42"})
	payloadStart := uint32(ftyp.Size() + 8) // ftyp + normal mdat header
	videoOffset := payloadStart
	audioOffset := payloadStart + uint32(len(videoBytes))

	moov := mp4.NewMoovBox()
	moov.AddChild(mp4.CreateMvhd())
	vtrak := addSingleChunkTrack(moov, 1, 1000, "vide", videoSizes, 250, videoOffset)
	atrak := addSingleChunkTrack(moov, 2, 1000, "soun", audioSizes, 125, audioOffset)
	moov.Trak = vtrak
	moov.Traks = []*mp4.TrakBox{vtrak, atrak}

	mdat := &mp4.MdatBox{}
	mdat.SetData(append(append([]byte(nil), videoBytes...), audioBytes...))

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
	return buf.Bytes(), videoBytes, audioBytes
}

func collectTrackSamples(t *testing.T, data []byte, trakIdx int) []byte {
	t.Helper()
	rs := bytes.NewReader(data)
	f, err := mp4.DecodeFile(rs, mp4.WithDecodeMode(mp4.DecModeLazyMdat))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	trak := f.Moov.Traks[trakIdx]
	n := trak.Mdia.Minf.Stbl.Stsz.GetNrSamples()
	var buf bytes.Buffer
	if err := f.CopySampleData(&buf, rs, trak, 1, n, nil); err != nil {
		t.Fatalf("CopySampleData: %v", err)
	}
	return buf.Bytes()
}

func boxTypes(f *mp4.File) []string {
	types := make([]string, 0, len(f.Children))
	for _, b := range f.Children {
		types = append(types, b.Type())
	}
	return types
}

func TestInterleaveReordersAndKeepsData(t *testing.T) {
	raw, videoBytes, audioBytes := buildInterleaveFixture(t, true)

	var out bytes.Buffer
	res, err := Interleave(bytes.NewReader(raw), &out, InterleaveOptions{WindowMs: 250})
	if err != nil {
		t.Fatalf("Interleave: %v", err)
	}
	if !res.Changed {
		t.Error("Changed = false, want true")
	}

	f, err := mp4.DecodeFile(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if got, want := boxTypes(f), []string{"ftyp", "moov", "mdat"}; !equalStrings(got, want) {
		t.Fatalf("output boxes = %v, want %v", got, want)
	}

	vOff := f.Moov.Traks[0].Mdia.Minf.Stbl.Stco.ChunkOffset
	aOff := f.Moov.Traks[1].Mdia.Minf.Stbl.Stco.ChunkOffset
	if len(vOff) != 4 {
		t.Errorf("video chunk count = %d, want 4", len(vOff))
	}
	if len(aOff) != 4 {
		t.Errorf("audio chunk count = %d, want 4", len(aOff))
	}
	for i := 0; i+1 < len(vOff) && i+1 < len(aOff); i++ {
		if !(vOff[i] < aOff[i] && aOff[i] < vOff[i+1]) {
			t.Errorf("chunks not interleaved at window %d: video=%v audio=%v", i, vOff, aOff)
		}
	}

	if got := collectTrackSamples(t, out.Bytes(), 0); !bytes.Equal(got, videoBytes) {
		t.Errorf("video sample bytes changed:\n got %v\nwant %v", got, videoBytes)
	}
	if got := collectTrackSamples(t, out.Bytes(), 1); !bytes.Equal(got, audioBytes) {
		t.Errorf("audio sample bytes changed:\n got %v\nwant %v", got, audioBytes)
	}
}

func TestInterleaveWindowChangesChunkCount(t *testing.T) {
	raw, _, _ := buildInterleaveFixture(t, false)

	var out bytes.Buffer
	if _, err := Interleave(bytes.NewReader(raw), &out, InterleaveOptions{WindowMs: 1000}); err != nil {
		t.Fatalf("Interleave: %v", err)
	}
	f, err := mp4.DecodeFile(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	// All samples fall in a single 1 s window, so each track has one chunk.
	if got := len(f.Moov.Traks[0].Mdia.Minf.Stbl.Stco.ChunkOffset); got != 1 {
		t.Errorf("video chunk count = %d, want 1", got)
	}
	if got := len(f.Moov.Traks[1].Mdia.Minf.Stbl.Stco.ChunkOffset); got != 1 {
		t.Errorf("audio chunk count = %d, want 1", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
