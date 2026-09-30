package normalize

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/Eyevinn/mp4ff/mp4"
)

// FragmentOptions controls the fragmented (fMP4) output.
type FragmentOptions struct {
	// FragmentMs is the target fragment duration in milliseconds. Defaults to 2000.
	FragmentMs int
	// NoSidx disables the top-level segment index. A sidx is written by default.
	NoSidx bool
}

// fragTrack carries everything needed to fragment one track.
type fragTrack struct {
	reader   *mp4.File    // pristine tree, used to read sample data
	trak     *mp4.TrakBox // track in the output init tree (sample tables cleared)
	readTrak *mp4.TrakBox // matching track in the pristine tree (original tables)
	id       uint32
	handler  string
	scale    uint32
	sizes    []uint32
	durs     []uint32
	cto      []int32
	sync     []bool
	dts      []uint64
}

// sampleRange is a contiguous run of samples of one track within a fragment.
type sampleRange struct {
	track int // index into the cursor slice returned by planFragments
	first uint32
	last  uint32
}

// Fragment converts a progressive MP4 into a single-file fragmented MP4
// (CMAF-like): ftyp + moov(with mvex) + optional sidx + repeated moof+mdat.
//
// The init moov keeps each track's sample descriptions but drops its sample
// tables; per-sample timing lives in the fragments. Media data is streamed
// through a temporary file so the top-level sidx can be written before the
// fragments it points at.
func Fragment(src ReadSeekerAt, dst io.Writer, opts FragmentOptions) (*Result, error) {
	if opts.FragmentMs <= 0 {
		opts.FragmentMs = 2000
	}

	fRead, err := decodeLazy(src)
	if err != nil {
		return nil, fmt.Errorf("decode mp4: %w", err)
	}
	fMeta, err := decodeLazy(src)
	if err != nil {
		return nil, fmt.Errorf("decode mp4: %w", err)
	}

	if fRead.IsFragmented() {
		return nil, fmt.Errorf("input is already fragmented")
	}
	if fMeta.Moov == nil || fMeta.Mdat == nil {
		return nil, fmt.Errorf("input is missing moov or mdat")
	}

	inputSize := int64(totalSize(fRead.Children))

	tracks, err := collectFragTracks(fRead, fMeta)
	if err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, fmt.Errorf("no tracks with samples found")
	}
	for _, t := range tracks {
		clearSampleTables(t.trak)
	}
	addMvex(fMeta.Moov, tracks)

	frags, cursors := planFragments(tracks, opts.FragmentMs)
	if len(frags) == 0 {
		return nil, fmt.Errorf("no fragments produced")
	}

	tmp, err := os.CreateTemp("", "mp4norm-frag-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	ref := referenceTrack(cursors)
	sidxRefs, fragBytes, err := writeFragments(tmp, src, cursors, frags, ref)
	if err != nil {
		return nil, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	ftyp := fMeta.Ftyp
	if ftyp == nil {
		ftyp = mp4.NewFtyp("iso5", 0x200, []string{"iso5", "iso6", "mp41"})
	} else {
		ftyp.AddCompatibleBrands([]string{"iso5", "iso6"})
	}

	bw := bufio.NewWriterSize(dst, 1<<20)
	if err := ftyp.Encode(bw); err != nil {
		return nil, fmt.Errorf("encode ftyp: %w", err)
	}
	if err := fMeta.Moov.Encode(bw); err != nil {
		return nil, fmt.Errorf("encode moov: %w", err)
	}
	var sidxSize uint64
	if !opts.NoSidx && ref != nil {
		sidx := &mp4.SidxBox{
			Version:                  1,
			ReferenceID:              1,
			Timescale:                ref.scale,
			EarliestPresentationTime: 0,
			FirstOffset:              0,
			SidxRefs:                 sidxRefs,
		}
		sidxSize = sidx.Size()
		if err := sidx.Encode(bw); err != nil {
			return nil, fmt.Errorf("encode sidx: %w", err)
		}
	}
	if _, err := io.Copy(bw, tmp); err != nil {
		return nil, fmt.Errorf("copy fragments: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return nil, fmt.Errorf("flush output: %w", err)
	}

	return &Result{
		InputSize:  inputSize,
		OutputSize: int64(ftyp.Size() + fMeta.Moov.Size() + sidxSize + fragBytes),
		Changed:    true,
	}, nil
}

// collectFragTracks gathers per-sample metadata from the pristine tree and
// pairs each track with its counterpart in the init tree.
func collectFragTracks(fRead, fMeta *mp4.File) ([]*fragTrack, error) {
	if len(fRead.Moov.Traks) != len(fMeta.Moov.Traks) {
		return nil, fmt.Errorf("track count mismatch between decodes")
	}
	var tracks []*fragTrack
	for i, trak := range fRead.Moov.Traks {
		if trak.Mdia == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil {
			continue
		}
		stbl := trak.Mdia.Minf.Stbl
		n := int(stbl.Stsz.GetNrSamples())
		if n == 0 {
			continue
		}
		scale := uint32(1000)
		if trak.Mdia.Mdhd != nil && trak.Mdia.Mdhd.Timescale != 0 {
			scale = trak.Mdia.Mdhd.Timescale
		}
		durs, err := stbl.Stts.SampleDurations(uint32(n)) //nolint:gosec // sample count
		if err != nil {
			return nil, fmt.Errorf("sample durations: %w", err)
		}
		t := &fragTrack{
			reader:   fRead,
			trak:     fMeta.Moov.Traks[i],
			readTrak: trak,
			id:       trak.Tkhd.TrackID,
			scale:    scale,
			sizes:    make([]uint32, n),
			durs:     durs,
			cto:      make([]int32, n),
			sync:     make([]bool, n),
			dts:      make([]uint64, n),
		}
		if trak.Mdia.Hdlr != nil {
			t.handler = trak.Mdia.Hdlr.HandlerType
		}
		var dt uint64
		for j := 0; j < n; j++ {
			t.sizes[j] = stbl.Stsz.GetSampleSize(j + 1) // one-based
			t.dts[j] = dt
			dt += uint64(durs[j])
		}
		if stbl.Ctts != nil {
			cto, err := stbl.Ctts.CompositionTimeOffsets(uint32(n)) //nolint:gosec // sample count
			if err != nil {
				return nil, fmt.Errorf("composition time offsets: %w", err)
			}
			copy(t.cto, cto)
		}
		if stbl.Stss != nil {
			sync, err := stbl.Stss.SampleIsSync(uint32(n)) //nolint:gosec // sample count
			if err != nil {
				return nil, fmt.Errorf("sync samples: %w", err)
			}
			copy(t.sync, sync)
		} else {
			for j := range t.sync {
				t.sync[j] = true
			}
		}
		tracks = append(tracks, t)
	}
	return tracks, nil
}

// clearSampleTables empties a track's sample tables so it can serve as an
// init segment track (sample timing moves into the fragments).
func clearSampleTables(trak *mp4.TrakBox) {
	stbl := trak.Mdia.Minf.Stbl
	if stbl.Stts != nil {
		stbl.Stts.SampleCount = nil
		stbl.Stts.SampleTimeDelta = nil
	}
	if stbl.Stsc != nil {
		stbl.Stsc.Entries = nil
		stbl.Stsc.SampleDescriptionID = nil
	}
	if stbl.Stsz != nil {
		stbl.Stsz.SampleNumber = 0
		stbl.Stsz.SampleUniformSize = 0
		stbl.Stsz.SampleSize = nil
	}
	if stbl.Stco != nil {
		stbl.Stco.ChunkOffset = nil
	}
	if stbl.Co64 != nil {
		stbl.Co64.ChunkOffset = nil
	}
	if stbl.Stss != nil {
		stbl.Children = removeChildType(stbl.Children, "stss")
		stbl.Stss = nil
	}
	if stbl.Ctts != nil {
		stbl.Children = removeChildType(stbl.Children, "ctts")
		stbl.Ctts = nil
	}
}

func removeChildType(children []mp4.Box, boxType string) []mp4.Box {
	out := children[:0]
	for _, b := range children {
		if b.Type() != boxType {
			out = append(out, b)
		}
	}
	return out
}

func addMvex(moov *mp4.MoovBox, tracks []*fragTrack) {
	mvex := mp4.NewMvexBox()
	for _, t := range tracks {
		mvex.AddChild(mp4.CreateTrex(t.id))
	}
	moov.AddChild(mvex)
}

func trackPriority(handler string) int {
	switch handler {
	case "vide":
		return 0
	case "soun":
		return 1
	default:
		return 2
	}
}

// planFragments groups samples into fragment windows and returns the windows
// together with the ordered cursor slice they index into.
func planFragments(tracks []*fragTrack, windowMs int) ([][]sampleRange, []*fragTrack) {
	var cursors []*fragTrack
	for _, t := range tracks {
		if len(t.sizes) > 0 {
			cursors = append(cursors, t)
		}
	}
	sort.SliceStable(cursors, func(i, j int) bool {
		return trackPriority(cursors[i].handler) < trackPriority(cursors[j].handler)
	})

	ptr := make([]int, len(cursors))
	windowOf := func(t *fragTrack, i int) uint64 {
		return t.dts[i] * 1000 / uint64(t.scale) / uint64(windowMs)
	}

	var frags [][]sampleRange
	for {
		var minW uint64
		found := false
		for ci, t := range cursors {
			if ptr[ci] >= len(t.sizes) {
				continue
			}
			w := windowOf(t, ptr[ci])
			if !found || w < minW {
				minW = w
				found = true
			}
		}
		if !found {
			break
		}
		var group []sampleRange
		for ci, t := range cursors {
			if ptr[ci] >= len(t.sizes) || windowOf(t, ptr[ci]) != minW {
				continue
			}
			start := ptr[ci]
			for ptr[ci] < len(t.sizes) && windowOf(t, ptr[ci]) == minW {
				ptr[ci]++
			}
			group = append(group, sampleRange{track: ci, first: uint32(start + 1), last: uint32(ptr[ci])}) //nolint:gosec // sample count
		}
		frags = append(frags, group)
	}
	return frags, cursors
}

func referenceTrack(cursors []*fragTrack) *fragTrack {
	if len(cursors) == 0 {
		return nil
	}
	return cursors[0] // video first, by priority order
}

// writeFragments builds and encodes each fragment, writes it to tmp and returns
// the sidx references plus the total fragment byte count.
func writeFragments(tmp *os.File, src ReadSeekerAt, cursors []*fragTrack, frags [][]sampleRange, ref *fragTrack) ([]mp4.SidxRef, uint64, error) {
	var refs []mp4.SidxRef
	var total uint64
	var ws []byte

	for seq, group := range frags {
		trackIDs := make([]uint32, 0, len(group))
		for _, r := range group {
			trackIDs = append(trackIDs, cursors[r.track].id)
		}
		frag, err := mp4.CreateMultiTrackFragment(uint32(seq+1), trackIDs)
		if err != nil {
			return nil, 0, fmt.Errorf("create fragment %d: %w", seq+1, err)
		}

		for _, r := range group {
			t := cursors[r.track]
			var buf bytes.Buffer
			if err := t.reader.CopySampleData(&buf, src, t.readTrak, r.first, r.last, ws); err != nil {
				return nil, 0, fmt.Errorf("copy samples %d-%d: %w", r.first, r.last, err)
			}
			data := buf.Bytes()
			for nr := r.first; nr <= r.last; nr++ {
				size := t.sizes[nr-1]
				flags := mp4.NonSyncSampleFlags
				if t.sync[nr-1] {
					flags = mp4.SyncSampleFlags
				}
				fs := mp4.FullSample{
					Sample: mp4.Sample{
						Flags:                 flags,
						Dur:                   t.durs[nr-1],
						Size:                  size,
						CompositionTimeOffset: t.cto[nr-1],
					},
					DecodeTime: t.dts[nr-1],
					Data:       data[:size],
				}
				data = data[size:]
				if err := frag.AddFullSampleToTrack(fs, t.id); err != nil {
					return nil, 0, fmt.Errorf("add sample %d: %w", nr, err)
				}
			}
		}

		var fragBuf bytes.Buffer
		if err := frag.Encode(&fragBuf); err != nil {
			return nil, 0, fmt.Errorf("encode fragment %d: %w", seq+1, err)
		}
		if _, err := tmp.Write(fragBuf.Bytes()); err != nil {
			return nil, 0, err
		}
		total += uint64(fragBuf.Len())
		refs = append(refs, mp4.SidxRef{
			ReferencedSize:     uint32(fragBuf.Len()), //nolint:gosec // fragment fits uint32
			SubSegmentDuration: fragmentDuration(group, cursors, ref),
			StartsWithSAP:      1,
			SAPType:            1,
		})
	}
	return refs, total, nil
}

// fragmentDuration returns the duration of the reference track's samples in a
// fragment, in the reference timescale.
func fragmentDuration(group []sampleRange, cursors []*fragTrack, ref *fragTrack) uint32 {
	if ref == nil {
		return 0
	}
	for _, r := range group {
		t := cursors[r.track]
		if t != ref {
			continue
		}
		var d uint32
		for nr := r.first; nr <= r.last; nr++ {
			d += t.durs[nr-1]
		}
		return d
	}
	return 0
}
