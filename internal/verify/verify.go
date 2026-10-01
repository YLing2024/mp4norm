// Package verify checks that a freshly normalized MP4 is actually safe to
// publish: it is parseable, its layout is the one we promised, and its timing
// and stream inventory still match the source. The normalizer is never trusted
// to grade its own output.
package verify

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/Eyevinn/mp4ff/mp4"

	"github.com/YLing2024/mp4norm/internal/isobmff"
	"github.com/YLing2024/mp4norm/internal/probe"
)

// Product verifies that productPath is a faithful, well-formed rewrite of
// srcPath. fragmented selects the expected layout: a fragmented MP4 must carry
// a sidx and at least one moof, while a progressive file must lead with moov.
//
// Every check is reported with a specific reason so a failure never leaves the
// caller guessing which promise was broken.
func Product(srcPath, productPath string, fragmented bool) error {
	prod, err := probe.Analyze(productPath)
	if err != nil {
		return fmt.Errorf("product cannot be parsed: %w", err)
	}

	for _, f := range prod.Findings {
		switch {
		case f.Severity == probe.SeverityCritical:
			return fmt.Errorf("product has a critical problem: %s", f.Message)
		case f.Code == "truncated-box":
			return fmt.Errorf("product is truncated: %s", f.Message)
		}
	}

	if fragmented {
		if isobmff.Count(prod.Boxes, "sidx") == 0 || isobmff.Count(prod.Boxes, "moof") == 0 {
			return fmt.Errorf("product is missing the expected fragmented layout (needs both sidx and moof)")
		}
	} else if prod.MoovPosition != probe.MoovFront {
		return fmt.Errorf("product layout is wrong: moov is %s, expected at the front", prod.MoovPosition)
	}

	src, err := inspect(srcPath)
	if err != nil {
		return fmt.Errorf("read source streams: %w", err)
	}
	out, err := inspect(productPath)
	if err != nil {
		return fmt.Errorf("read product streams: %w", err)
	}

	if !equalTracks(src.tracks, out.tracks) {
		return fmt.Errorf("product streams differ from the source: [%s] vs [%s]",
			trackList(src.tracks), trackList(out.tracks))
	}

	// One frame of slack: the tolerance is the longest source video sample, so
	// a whole-frame shift is tolerated but two frames are not.
	tol := src.frameSec
	if diff := math.Abs(src.durationSec - out.durationSec); diff > tol+1e-6 {
		return fmt.Errorf("product duration %.3fs differs from the source %.3fs by more than one frame (%.3fs)",
			out.durationSec, src.durationSec, tol)
	}
	return nil
}

// track is the minimal identity of a media track that a lossless rewrite must
// preserve: its handler (video/audio/...) and its sample-entry codec.
type track struct {
	handler string
	codec   string
}

// detail carries the moov-derived facts used by verification.
type detail struct {
	durationSec float64
	frameSec    float64
	tracks      []track
}

// inspect decodes only the leading portion of the file up to the end of moov,
// so fragmented outputs (thousands of moof boxes) are not walked in full. mdat
// payloads are always skipped via lazy decoding.
func inspect(path string) (*detail, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	boxes, _, err := isobmff.ScanTopLevel(f, fi.Size())
	if err != nil {
		return nil, err
	}
	moov := isobmff.Find(boxes, "moov")
	if moov == nil {
		return nil, fmt.Errorf("%s: no moov box", path)
	}

	section := io.NewSectionReader(f, 0, moov.End())
	file, err := mp4.DecodeFile(section, mp4.WithDecodeMode(mp4.DecModeLazyMdat))
	if err != nil {
		return nil, fmt.Errorf("decode moov: %w", err)
	}

	d := &detail{}
	d.durationSec, d.frameSec = timingOf(file)
	d.tracks = tracksOf(file)
	return d, nil
}

// timingOf returns the overall presentation duration and the longest video
// sample duration, both in seconds. The movie header is authoritative for
// fragmented files, where per-track sample tables are empty.
func timingOf(f *mp4.File) (duration, frame float64) {
	if f.Moov == nil {
		return 0, 0
	}
	if m := f.Moov.Mvhd; m != nil && m.Timescale > 0 && m.Duration > 0 {
		duration = float64(m.Duration) / float64(m.Timescale)
	}
	for _, t := range f.Moov.Traks {
		if t == nil || t.Mdia == nil {
			continue
		}
		md := t.Mdia.Mdhd
		if md == nil || md.Timescale == 0 {
			continue
		}
		if md.Duration > 0 {
			if d := float64(md.Duration) / float64(md.Timescale); d > duration {
				duration = d
			}
		}
		if t.Mdia.Hdlr != nil && t.Mdia.Hdlr.HandlerType == "vide" {
			if d := longestSample(t, md.Timescale); d > frame {
				frame = d
			}
		}
	}
	return duration, frame
}

// longestSample returns the longest stts time delta of a track in seconds.
func longestSample(t *mp4.TrakBox, timescale uint32) float64 {
	if t.Mdia == nil || t.Mdia.Minf == nil || t.Mdia.Minf.Stbl == nil {
		return 0
	}
	stts := t.Mdia.Minf.Stbl.Stts
	if stts == nil {
		return 0
	}
	var maxDelta uint32
	for _, d := range stts.SampleTimeDelta {
		if d > maxDelta {
			maxDelta = d
		}
	}
	if maxDelta == 0 || timescale == 0 {
		return 0
	}
	return float64(maxDelta) / float64(timescale)
}

func tracksOf(f *mp4.File) []track {
	if f.Moov == nil {
		return nil
	}
	out := make([]track, 0, len(f.Moov.Traks))
	for _, t := range f.Moov.Traks {
		if t == nil || t.Mdia == nil {
			continue
		}
		tr := track{}
		if t.Mdia.Hdlr != nil {
			tr.handler = t.Mdia.Hdlr.HandlerType
		}
		if t.Mdia.Minf != nil && t.Mdia.Minf.Stbl != nil && t.Mdia.Minf.Stbl.Stsd != nil {
			if ch := t.Mdia.Minf.Stbl.Stsd.Children; len(ch) > 0 {
				tr.codec = ch[0].Type()
			}
		}
		out = append(out, tr)
	}
	return out
}

func equalTracks(a, b []track) bool {
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

func trackList(ts []track) string {
	parts := make([]string, 0, len(ts))
	for _, t := range ts {
		codec := t.codec
		if codec == "" {
			codec = "?"
		}
		parts = append(parts, t.handler+"/"+codec)
	}
	return strings.Join(parts, ", ")
}
