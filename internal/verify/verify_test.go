package verify

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"

	"github.com/YLing2024/mp4norm/internal/normalize"
)

// trakWithDuration builds a one-sample track whose mdhd advertises durSec.
func trakWithDuration(handler string, durSec float64, timescale uint32) *mp4.TrakBox {
	tr := mp4.CreateEmptyTrak(1, timescale, handler, "und")
	tr.Mdia.Mdhd.Duration = uint64(durSec * float64(timescale))
	stbl := tr.Mdia.Minf.Stbl
	stbl.Stts.SampleCount = []uint32{1}
	stbl.Stts.SampleTimeDelta = []uint32{3000}
	stbl.Stsc.Entries = []mp4.StscEntry{{FirstChunk: 1, SamplesPerChunk: 1}}
	stbl.Stsc.SampleDescriptionID = []uint32{1}
	stbl.Stsz.SampleNumber = 1
	stbl.Stsz.SampleSize = []uint32{4}
	stbl.Stco.ChunkOffset = []uint32{0}
	return tr
}

// buildFile encodes a minimal progressive MP4 with the given tracks.
func buildFile(t *testing.T, moovAtEnd bool, traks ...*mp4.TrakBox) []byte {
	t.Helper()
	ftyp := mp4.NewFtyp("isom", 0x200, []string{"isom", "mp42"})
	moov := mp4.NewMoovBox()
	moov.AddChild(mp4.CreateMvhd())
	var media []byte
	for _, tr := range traks {
		moov.AddChild(tr)
		moov.Traks = append(moov.Traks, tr)
		media = append(media, 0x55, 0x55, 0x55, 0x55)
	}
	mdat := &mp4.MdatBox{}
	mdat.SetData(media)

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
	return buf.Bytes()
}

func writeData(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestProductAcceptsProgressive(t *testing.T) {
	src := writeData(t, "src.mp4", buildFile(t, true, trakWithDuration("vide", 5, 90000)))
	prod := writeData(t, "prod.mp4", buildFile(t, false, trakWithDuration("vide", 5, 90000)))
	if err := Product(src, prod, false); err != nil {
		t.Fatalf("Product = %v, want nil", err)
	}
}

func TestProductRejectsMoovAtEnd(t *testing.T) {
	src := writeData(t, "src.mp4", buildFile(t, true, trakWithDuration("vide", 5, 90000)))
	prod := writeData(t, "prod.mp4", buildFile(t, true, trakWithDuration("vide", 5, 90000)))
	if err := Product(src, prod, false); err == nil {
		t.Fatal("Product = nil, want a layout error")
	}
}

func TestProductRejectsMissingFragmentIndex(t *testing.T) {
	src := writeData(t, "src.mp4", buildFile(t, true, trakWithDuration("vide", 5, 90000)))
	prod := writeData(t, "prod.mp4", buildFile(t, false, trakWithDuration("vide", 5, 90000)))
	if err := Product(src, prod, true); err == nil {
		t.Fatal("Product = nil, want a fragmented-layout error")
	}
}

func TestProductRejectsTruncated(t *testing.T) {
	src := writeData(t, "src.mp4", buildFile(t, true, trakWithDuration("vide", 5, 90000)))
	prod := buildFile(t, false, trakWithDuration("vide", 5, 90000))
	prod = prod[:len(prod)-12]
	prodPath := writeData(t, "prod.mp4", prod)
	if err := Product(src, prodPath, false); err == nil {
		t.Fatal("Product = nil, want a truncation error")
	}
}

func TestProductRejectsDurationMismatch(t *testing.T) {
	src := writeData(t, "src.mp4", buildFile(t, true, trakWithDuration("vide", 5, 90000)))
	prod := writeData(t, "prod.mp4", buildFile(t, false, trakWithDuration("vide", 10, 90000)))
	err := Product(src, prod, false)
	if err == nil {
		t.Fatal("Product = nil, want a duration error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("duration")) {
		t.Fatalf("error = %v, want it to mention duration", err)
	}
}

func TestProductRejectsTrackMismatch(t *testing.T) {
	src := writeData(t, "src.mp4", buildFile(t, true, trakWithDuration("vide", 5, 90000)))
	prod := writeData(t, "prod.mp4", buildFile(t, false, trakWithDuration("soun", 5, 90000)))
	err := Product(src, prod, false)
	if err == nil {
		t.Fatal("Product = nil, want a stream mismatch error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("streams")) {
		t.Fatalf("error = %v, want it to mention streams", err)
	}
}

func TestProductAcceptsFragment(t *testing.T) {
	raw := buildFile(t, true, trakWithDuration("vide", 5, 90000))
	var out bytes.Buffer
	if _, err := normalize.Fragment(bytes.NewReader(raw), &out, normalize.FragmentOptions{FragmentMs: 2000}); err != nil {
		t.Fatalf("Fragment: %v", err)
	}
	src := writeData(t, "src.mp4", raw)
	prod := writeData(t, "prod.mp4", out.Bytes())
	if err := Product(src, prod, true); err != nil {
		t.Fatalf("Product(fragmented) = %v, want nil", err)
	}
}
