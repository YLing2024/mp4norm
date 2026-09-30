// Package normalize rewrites MP4 container layout so that files open instantly
// and seek quickly, without touching the media data.
package normalize

import (
	"bufio"
	"fmt"
	"io"
	"math"

	"github.com/Eyevinn/mp4ff/mp4"
)

// DefaultCopyBufferSize is the buffer used to stream media data from source to
// destination. A large buffer keeps the lossless path I/O-bound and avoids
// per-sample syscalls.
const DefaultCopyBufferSize = 4 << 20 // 4 MiB

// Result describes what a normalization pass did.
type Result struct {
	// InputSize is the total size of the source file.
	InputSize int64
	// OutputSize is the total size of the written file.
	OutputSize int64
	// Delta is how far the mdat payload moved (new offset minus old offset).
	// All chunk offsets are shifted by this amount.
	Delta int64
	// Changed reports whether the box order or offsets had to be modified.
	Changed bool
}

// ReadSeekerAt is the source access required by normalization: sequential
// seeking for parsing and random access for streaming media data. Both
// *os.File and *bytes.Reader satisfy it.
type ReadSeekerAt interface {
	io.ReadSeeker
	io.ReaderAt
}

// Faststart rewrites a progressive MP4 so that the moov box comes before the
// mdat box ("faststart"). Media data is streamed, never buffered whole, so the
// pass stays I/O-bound even on multi-gigabyte files.
//
// src may be positioned anywhere; it is accessed via ReadAt.
// dst receives the rewritten file.
func Faststart(src ReadSeekerAt, dst io.Writer) (*Result, error) {
	f, err := mp4.DecodeFile(src, mp4.WithDecodeMode(mp4.DecModeLazyMdat))
	if err != nil {
		return nil, fmt.Errorf("decode mp4: %w", err)
	}
	if f.IsFragmented() {
		return nil, fmt.Errorf("input is a fragmented MP4; use the fragmented output path")
	}
	if f.Moov == nil {
		return nil, fmt.Errorf("input has no moov box")
	}
	if f.Mdat == nil {
		return nil, fmt.Errorf("input has no mdat box")
	}

	order := reorder(f)

	// New absolute offset of the mdat payload.
	var newMdatStart uint64
	for _, b := range order {
		if b == mp4.Box(f.Mdat) {
			break
		}
		newMdatStart += b.Size()
	}
	oldPayload := f.Mdat.PayloadAbsoluteOffset()
	newPayload := newMdatStart + f.Mdat.HeaderSize()
	delta := int64(newPayload) - int64(oldPayload) //nolint:gosec // offsets are well below int64 max

	if delta != 0 {
		if err := shiftChunkOffsets(f.Moov, delta); err != nil {
			return nil, err
		}
	}

	res := &Result{
		InputSize:  int64(totalSize(f.Children)),
		OutputSize: int64(totalSize(order)),
		Delta:      delta,
		Changed:    delta != 0 || !sameOrder(f.Children, order),
	}
	if err := writeFile(src, dst, order, f.Mdat, oldPayload); err != nil {
		return nil, err
	}
	return res, nil
}

// reorder returns the boxes in faststart order:
//
//	ftyp, moov, <other boxes>, mdat
func reorder(f *mp4.File) []mp4.Box {
	order := make([]mp4.Box, 0, len(f.Children)+1)
	if f.Ftyp != nil {
		order = append(order, f.Ftyp)
	}
	order = append(order, f.Moov)
	for _, b := range f.Children {
		if b == mp4.Box(f.Ftyp) || b == mp4.Box(f.Moov) || b == mp4.Box(f.Mdat) {
			continue
		}
		order = append(order, b)
	}
	order = append(order, f.Mdat)
	return order
}

// shiftChunkOffsets adds delta to every chunk offset in every track.
func shiftChunkOffsets(moov *mp4.MoovBox, delta int64) error {
	for _, trak := range moov.Traks {
		if trak == nil || trak.Mdia == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil {
			continue
		}
		stbl := trak.Mdia.Minf.Stbl
		if stbl.Stco != nil {
			for i, off := range stbl.Stco.ChunkOffset {
				n := int64(off) + delta
				if n < 0 || n > math.MaxUint32 {
					return fmt.Errorf("chunk offset %d would leave 32-bit range after shift (%d)", off, n)
				}
				stbl.Stco.ChunkOffset[i] = uint32(n)
			}
		}
		if stbl.Co64 != nil {
			for i, off := range stbl.Co64.ChunkOffset {
				n := int64(off) + delta
				if n < 0 {
					return fmt.Errorf("chunk offset %d would become negative after shift", off)
				}
				stbl.Co64.ChunkOffset[i] = uint64(n)
			}
		}
	}
	return nil
}

// writeFile encodes each box in order, streaming the mdat payload from src.
func writeFile(src io.ReaderAt, dst io.Writer, order []mp4.Box, mdat *mp4.MdatBox, oldPayload uint64) error {
	bw := bufio.NewWriterSize(dst, 1<<20)
	for _, b := range order {
		if b == mp4.Box(mdat) {
			if err := mdat.Encode(bw); err != nil {
				return fmt.Errorf("encode mdat header: %w", err)
			}
			sr := io.NewSectionReader(src, int64(oldPayload), int64(mdat.GetLazyDataSize())) //nolint:gosec // bounded by file size
			if _, err := io.CopyBuffer(bw, sr, make([]byte, DefaultCopyBufferSize)); err != nil {
				return fmt.Errorf("copy mdat payload: %w", err)
			}
			continue
		}
		if err := b.Encode(bw); err != nil {
			return fmt.Errorf("encode %s: %w", b.Type(), err)
		}
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("flush output: %w", err)
	}
	return nil
}

func totalSize(boxes []mp4.Box) uint64 {
	var n uint64
	for _, b := range boxes {
		n += b.Size()
	}
	return n
}

func sameOrder(a, b []mp4.Box) bool {
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
