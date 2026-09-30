package normalize

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/Eyevinn/mp4ff/mp4"
)

// InterleaveOptions controls the interleaving rewrite.
type InterleaveOptions struct {
	// WindowMs is the target time bucket for interleaving audio and video,
	// in milliseconds. Samples whose decode time falls in the same bucket are
	// written close together. A small window (0.5-1.0 s) keeps range requests
	// local when seeking. Defaults to 1000 ms.
	WindowMs int
}

// trackPlan is the new chunk layout for one track.
type trackPlan struct {
	trak     *mp4.TrakBox // track in the output tree (tables are rewritten)
	readTrak *mp4.TrakBox // matching track in the pristine tree (original tables)
	origIdx  int          // index of the track within moov.Traks
	chunks   []plannedChunk
	stco     *mp4.StcoBox
	co64     *mp4.Co64Box
}

type plannedChunk struct {
	first, last uint32 // 1-based inclusive sample numbers
	size        uint64
	offset      uint64 // absolute file offset, filled in later
}

type chunkRef struct {
	track int
	chunk int
}

// Interleave rewrites a progressive MP4 so that:
//
//   - moov precedes mdat (faststart), and
//   - audio and video samples are re-bucketed into ~WindowMs interleave windows.
//
// Each track's sample order, durations, sizes and sync samples are preserved,
// so only the chunk structure (stsc/stco) and the physical byte order change.
// Media data is streamed, never buffered whole.
func Interleave(src ReadSeekerAt, dst io.Writer, opts InterleaveOptions) (*Result, error) {
	if opts.WindowMs <= 0 {
		opts.WindowMs = 1000
	}

	fRead, err := decodeLazy(src)
	if err != nil {
		return nil, fmt.Errorf("decode mp4: %w", err)
	}
	fOut, err := decodeLazy(src)
	if err != nil {
		return nil, fmt.Errorf("decode mp4: %w", err)
	}

	if fOut.IsFragmented() {
		return nil, fmt.Errorf("input is a fragmented MP4; use the fragmented output path")
	}
	if fOut.Moov == nil {
		return nil, fmt.Errorf("input has no moov box")
	}
	if fOut.Mdat == nil {
		return nil, fmt.Errorf("input has no mdat box")
	}

	inputSize := int64(totalSize(fRead.Children))

	order, plans, err := planInterleave(fOut.Moov, opts.WindowMs)
	if err != nil {
		return nil, err
	}
	for _, p := range plans {
		if p.origIdx < len(fRead.Moov.Traks) {
			p.readTrak = fRead.Moov.Traks[p.origIdx]
		}
		if p.readTrak == nil {
			return nil, fmt.Errorf("cannot match track %d in source", p.origIdx)
		}
	}
	if err := applySampleTables(plans); err != nil {
		return nil, err
	}

	var newTotal uint64
	for _, p := range plans {
		for _, c := range p.chunks {
			newTotal += c.size
		}
	}
	fOut.Mdat.SetLazyDataSize(newTotal)

	outOrder := reorder(fOut)
	var head uint64
	for _, b := range outOrder {
		if b == mp4.Box(fOut.Mdat) {
			break
		}
		head += b.Size()
	}
	payloadStart := head + fOut.Mdat.HeaderSize()

	running := payloadStart
	for _, ref := range order {
		c := &plans[ref.track].chunks[ref.chunk]
		c.offset = running
		running += c.size
	}
	if err := setChunkOffsets(plans); err != nil {
		return nil, err
	}

	oldPayload := fRead.Mdat.PayloadAbsoluteOffset()
	res := &Result{
		InputSize:  inputSize,
		OutputSize: int64(totalSize(outOrder)),
		Delta:      int64(payloadStart) - int64(oldPayload), //nolint:gosec // offsets well below int64 max
		Changed:    true,
	}

	bw := bufio.NewWriterSize(dst, 1<<20)
	ws := make([]byte, DefaultCopyBufferSize)
	for _, b := range outOrder {
		if b == mp4.Box(fOut.Mdat) {
			if err := fOut.Mdat.Encode(bw); err != nil {
				return nil, fmt.Errorf("encode mdat header: %w", err)
			}
			for _, ref := range order {
				p := plans[ref.track]
				c := p.chunks[ref.chunk]
				if err := fRead.CopySampleData(bw, src, p.readTrak, c.first, c.last, ws); err != nil {
					return nil, fmt.Errorf("copy samples %d-%d: %w", c.first, c.last, err)
				}
			}
			continue
		}
		if err := b.Encode(bw); err != nil {
			return nil, fmt.Errorf("encode %s: %w", b.Type(), err)
		}
	}
	if err := bw.Flush(); err != nil {
		return nil, fmt.Errorf("flush output: %w", err)
	}
	return res, nil
}

// planInterleave computes, for each track, the new chunks, and the global order
// in which chunks are written to mdat.
func planInterleave(moov *mp4.MoovBox, windowMs int) ([]chunkRef, []*trackPlan, error) {
	type cursor struct {
		plan   *trackPlan
		prefix []uint64 // prefix[i] = total size of samples [0,i)
		dts    []uint64
		scale  uint32
		ptr    int
		n      int
	}

	var cursors []*cursor
	for ti, trak := range moov.Traks {
		if trak == nil || trak.Mdia == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil {
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
		durs, err := stbl.Stts.SampleDurations(uint32(n)) //nolint:gosec // n is a sample count
		if err != nil {
			return nil, nil, fmt.Errorf("sample durations: %w", err)
		}
		c := &cursor{plan: &trackPlan{trak: trak, origIdx: ti}, scale: scale, n: n}
		c.dts = make([]uint64, n)
		c.prefix = make([]uint64, n+1)
		var t uint64
		for i := 0; i < n; i++ {
			c.dts[i] = t
			t += uint64(durs[i])
			// Stsz.GetSampleSize is one-based.
			c.prefix[i+1] = c.prefix[i] + uint64(stbl.Stsz.GetSampleSize(i+1))
		}
		cursors = append(cursors, c)
	}
	if len(cursors) == 0 {
		return nil, nil, fmt.Errorf("no samples found in any track")
	}

	// Video first, then audio, then everything else; stable within groups.
	priority := func(t *mp4.TrakBox) int {
		if t.Mdia != nil && t.Mdia.Hdlr != nil {
			switch t.Mdia.Hdlr.HandlerType {
			case "vide":
				return 0
			case "soun":
				return 1
			}
		}
		return 2
	}
	sort.SliceStable(cursors, func(i, j int) bool {
		return priority(cursors[i].plan.trak) < priority(cursors[j].plan.trak)
	})

	windowOf := func(c *cursor, i int) uint64 {
		ms := c.dts[i] * 1000 / uint64(c.scale)
		return ms / uint64(windowMs)
	}

	var order []chunkRef
	plans := make([]*trackPlan, len(cursors))

	for {
		// Smallest not-yet-emitted window across all tracks.
		var minW uint64
		found := false
		for _, c := range cursors {
			if c.ptr >= c.n {
				continue
			}
			w := windowOf(c, c.ptr)
			if !found || w < minW {
				minW = w
				found = true
			}
		}
		if !found {
			break
		}
		for ti, c := range cursors {
			if c.ptr >= c.n || windowOf(c, c.ptr) != minW {
				continue
			}
			start := c.ptr
			for c.ptr < c.n && windowOf(c, c.ptr) == minW {
				c.ptr++
			}
			c.plan.chunks = append(c.plan.chunks, plannedChunk{
				first: uint32(start + 1),
				last:  uint32(c.ptr), //nolint:gosec // sample count fits uint32
				size:  c.prefix[c.ptr] - c.prefix[start],
			})
			order = append(order, chunkRef{track: ti, chunk: len(c.plan.chunks) - 1})
		}
	}

	for i, c := range cursors {
		plans[i] = c.plan
	}
	return order, plans, nil
}

// applySampleTables builds fresh stsc and stco/co64 boxes for each track and
// swaps them into the stbl children, which is what is actually encoded.
func applySampleTables(plans []*trackPlan) error {
	for _, p := range plans {
		stbl := p.trak.Mdia.Minf.Stbl

		counts := make([]uint32, len(p.chunks))
		for i, c := range p.chunks {
			counts[i] = c.last - c.first + 1
		}
		descID := stbl.Stsc.GetSampleDescriptionID(1)
		if descID == 0 {
			descID = 1
		}
		stsc, err := buildStsc(counts, descID)
		if err != nil {
			return err
		}
		if !replaceChild(stbl.Children, "stsc", stsc) {
			return fmt.Errorf("track has no stsc box")
		}
		stbl.Stsc = stsc

		switch {
		case stbl.Stco != nil:
			stco := &mp4.StcoBox{ChunkOffset: make([]uint32, len(p.chunks))}
			replaceChild(stbl.Children, "stco", stco)
			stbl.Stco = stco
			stbl.Co64 = nil
			p.stco = stco
		case stbl.Co64 != nil:
			co64 := &mp4.Co64Box{ChunkOffset: make([]uint64, len(p.chunks))}
			replaceChild(stbl.Children, "co64", co64)
			stbl.Co64 = co64
			stbl.Stco = nil
			p.co64 = co64
		default:
			return fmt.Errorf("track has neither stco nor co64 box")
		}
	}
	return nil
}

// buildStsc collapses runs of chunks with an equal sample count into stsc
// entries, as the box is defined in terms of such runs.
func buildStsc(counts []uint32, sampleDescriptionID uint32) (*mp4.StscBox, error) {
	b := &mp4.StscBox{}
	for i := 0; i < len(counts); {
		j := i
		for j+1 < len(counts) && counts[j+1] == counts[i] {
			j++
		}
		if err := b.AddEntry(uint32(i+1), counts[i], sampleDescriptionID); err != nil {
			return nil, err
		}
		i = j + 1
	}
	return b, nil
}

func setChunkOffsets(plans []*trackPlan) error {
	for _, p := range plans {
		for i, c := range p.chunks {
			switch {
			case p.stco != nil:
				if c.offset > math.MaxUint32 {
					return fmt.Errorf("chunk offset %d exceeds 32-bit stco range; co64 needed", c.offset)
				}
				p.stco.ChunkOffset[i] = uint32(c.offset)
			case p.co64 != nil:
				p.co64.ChunkOffset[i] = c.offset
			}
		}
	}
	return nil
}

// replaceChild swaps the first child box of the given type in place, keeping
// the position in the container's child ordering.
func replaceChild(children []mp4.Box, boxType string, newBox mp4.Box) bool {
	for i, b := range children {
		if b.Type() == boxType {
			children[i] = newBox
			return true
		}
	}
	return false
}

func decodeLazy(src io.ReadSeeker) (*mp4.File, error) {
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek to start: %w", err)
	}
	return mp4.DecodeFile(src, mp4.WithDecodeMode(mp4.DecModeLazyMdat))
}
