package normalize

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
)

// buildScaledFixture builds a two-track progressive MP4 with the requested
// sample counts, mdat storing all video then all audio (not interleaved).
func buildScaledFixture(tb testing.TB, nVideo, nAudio int) []byte {
	tb.Helper()

	videoSizes := make([]uint32, nVideo)
	for i := range videoSizes {
		videoSizes[i] = 1000 // ~ typical video sample
	}
	audioSizes := make([]uint32, nAudio)
	for i := range audioSizes {
		audioSizes[i] = 300
	}

	videoBytes := concatSamples(0x10, videoSizes)
	audioBytes := concatSamples(0x60, audioSizes)

	ftyp := mp4.NewFtyp("isom", 0x200, []string{"isom", "mp42"})
	payloadStart := uint32(ftyp.Size() + 8)
	videoOffset := payloadStart
	audioOffset := payloadStart + uint32(len(videoBytes))

	moov := mp4.NewMoovBox()
	moov.AddChild(mp4.CreateMvhd())
	vtrak := addSingleChunkTrack(moov, 1, 15360, "vide", videoSizes, 512, videoOffset)
	atrak := addSingleChunkTrack(moov, 2, 44100, "soun", audioSizes, 1024, audioOffset)
	moov.Trak = vtrak
	moov.Traks = []*mp4.TrakBox{vtrak, atrak}

	mdat := &mp4.MdatBox{}
	mdat.SetData(append(append([]byte(nil), videoBytes...), audioBytes...))

	f := mp4.NewFile()
	f.Ftyp = ftyp
	f.Moov = moov
	f.Mdat = mdat
	f.Children = []mp4.Box{ftyp, mdat, moov}

	var buf bytes.Buffer
	if err := f.Encode(&buf); err != nil {
		tb.Fatalf("encode fixture: %v", err)
	}
	return buf.Bytes()
}

// benchmarkOnDisk runs fn(N times) against a real file so the numbers include
// file I/O, which is what dominates the lossless path in practice.
func benchmarkOnDisk(b *testing.B, nVideo, nAudio int, fn func(src *os.File, dst io.Writer) error) {
	raw := buildScaledFixture(b, nVideo, nAudio)
	dir := b.TempDir()
	inPath := filepath.Join(dir, "in.mp4")
	if err := os.WriteFile(inPath, raw, 0o600); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := os.Open(inPath)
		if err != nil {
			b.Fatal(err)
		}
		out, err := os.CreateTemp(dir, "out-*")
		if err != nil {
			b.Fatal(err)
		}
		if err := fn(in, out); err != nil {
			b.Fatal(err)
		}
		out.Close()
		in.Close()
		os.Remove(out.Name())
	}
}

func BenchmarkFaststart(b *testing.B) {
	benchmarkOnDisk(b, 30000, 43000, func(src *os.File, dst io.Writer) error {
		_, err := Faststart(src, dst)
		return err
	})
}

func BenchmarkInterleave(b *testing.B) {
	benchmarkOnDisk(b, 30000, 43000, func(src *os.File, dst io.Writer) error {
		_, err := Interleave(src, dst, InterleaveOptions{WindowMs: 1000})
		return err
	})
}
