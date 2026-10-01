package classify

import (
	"os"

	"github.com/Eyevinn/mp4ff/mp4"
)

// span is a byte range occupied by one track's chunks.
type span struct{ lo, hi uint64 }

// NotInterleaved reports, best-effort, whether a progressive MP4 stores a
// video track and an audio track as two separate runs in the file instead of
// interleaving them. Errors are swallowed and reported as false: interleaving
// is an advisory check that must never reject a file.
//
// Only the moov sample tables are read (via the lazy decoder), so the check
// stays cheap even on multi-gigabyte files. It is deliberately conservative:
// it needs two media tracks, each split into at least two chunks, whose byte
// spans do not overlap at all.
func NotInterleaved(path string) (result bool) {
	// A parser panic on a malformed moov must never take down a scan.
	defer func() {
		if recover() != nil {
			result = false
		}
	}()

	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	mf, err := mp4.DecodeFile(f, mp4.WithDecodeMode(mp4.DecModeLazyMdat))
	if err != nil {
		return false
	}
	if mf.IsFragmented() || mf.Moov == nil {
		return false
	}

	var spans []span
	for _, trak := range mf.Moov.Traks {
		if trak == nil || trak.Mdia == nil || trak.Mdia.Hdlr == nil ||
			trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil {
			continue
		}
		switch trak.Mdia.Hdlr.HandlerType {
		case "vide", "soun":
		default:
			continue
		}
		offs := chunkOffsets(trak.Mdia.Minf.Stbl)
		if len(offs) < 2 {
			continue
		}
		lo, hi := offs[0], offs[0]
		for _, o := range offs {
			if o < lo {
				lo = o
			}
			if o > hi {
				hi = o
			}
		}
		spans = append(spans, span{lo: lo, hi: hi})
	}
	if len(spans) < 2 {
		return false
	}
	for i := 0; i < len(spans); i++ {
		for j := i + 1; j < len(spans); j++ {
			if spans[i].hi < spans[j].lo || spans[j].hi < spans[i].lo {
				return true
			}
		}
	}
	return false
}

// chunkOffsets returns the chunk file offsets of a sample table, using either
// the 32-bit (stco) or 64-bit (co64) table.
func chunkOffsets(stbl *mp4.StblBox) []uint64 {
	if stbl.Stco != nil {
		out := make([]uint64, len(stbl.Stco.ChunkOffset))
		for i, o := range stbl.Stco.ChunkOffset {
			out[i] = uint64(o)
		}
		return out
	}
	if stbl.Co64 != nil {
		return stbl.Co64.ChunkOffset
	}
	return nil
}
