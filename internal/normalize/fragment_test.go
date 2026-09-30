package normalize

import (
	"bytes"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
)

func TestFragmentRoundTrip(t *testing.T) {
	raw, videoBytes, audioBytes := buildInterleaveFixture(t, true)

	var out bytes.Buffer
	res, err := Fragment(bytes.NewReader(raw), &out, FragmentOptions{FragmentMs: 250})
	if err != nil {
		t.Fatalf("Fragment: %v", err)
	}
	if !res.Changed {
		t.Error("Changed = false, want true")
	}

	fOut, err := mp4.DecodeFile(bytes.NewReader(out.Bytes()), mp4.WithDecodeMode(mp4.DecModeLazyMdat))
	if err != nil {
		t.Fatalf("decode fragmented output: %v", err)
	}
	if !fOut.IsFragmented() {
		t.Fatal("output is not fragmented")
	}
	if fOut.Init == nil || fOut.Init.Moov == nil {
		t.Fatal("output has no init segment moov")
	}
	if fOut.Sidx == nil {
		t.Error("output has no top-level sidx")
	}
	if len(fOut.Segments) != 4 {
		t.Errorf("segment count = %d, want 4", len(fOut.Segments))
	}

	// Round-trip: defragment and compare each track's sample bytes.
	rs := bytes.NewReader(out.Bytes())
	var prog bytes.Buffer
	if err := mp4.Defragment(fOut, rs, &prog); err != nil {
		t.Fatalf("Defragment: %v", err)
	}
	if got := collectTrackSamples(t, prog.Bytes(), 0); !bytes.Equal(got, videoBytes) {
		t.Errorf("video samples changed after fragment round-trip:\n got %v\nwant %v", got, videoBytes)
	}
	if got := collectTrackSamples(t, prog.Bytes(), 1); !bytes.Equal(got, audioBytes) {
		t.Errorf("audio samples changed after fragment round-trip:\n got %v\nwant %v", got, audioBytes)
	}
}

func TestFragmentNoSidx(t *testing.T) {
	raw, _, _ := buildInterleaveFixture(t, false)
	var out bytes.Buffer
	if _, err := Fragment(bytes.NewReader(raw), &out, FragmentOptions{FragmentMs: 500, NoSidx: true}); err != nil {
		t.Fatalf("Fragment: %v", err)
	}
	f, err := mp4.DecodeFile(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !f.IsFragmented() {
		t.Fatal("output is not fragmented")
	}
	if f.Sidx != nil {
		t.Error("sidx present, want none")
	}
}
