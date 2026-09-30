// Package isobmff provides minimal ISO Base Media File Format (MP4) box-level
// scanning. It intentionally only walks top-level boxes and never decodes media
// payloads, which makes it cheap enough to run on very large files.
package isobmff

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Box describes a single top-level ISO-BMFF box (atom).
type Box struct {
	// Type is the 4-character box type, e.g. "ftyp", "moov", "mdat".
	Type string
	// Offset is the absolute byte offset of the box header in the file.
	Offset int64
	// Size is the total box size including its header.
	Size int64
	// HeaderSize is 8 for a normal box, 16 for a 64-bit (largesize) box.
	HeaderSize int64
}

// End returns the offset just past the box.
func (b Box) End() int64 { return b.Offset + b.Size }

const (
	normalHeaderSize = 8
	largeHeaderSize  = 16
)

// ScanTopLevel walks the top-level boxes of an ISO-BMFF file.
//
// r must cover at least fileSize bytes starting at offset 0. Only box headers
// are read, so this is safe and fast for multi-gigabyte files.
func ScanTopLevel(r io.ReaderAt, fileSize int64) ([]Box, error) {
	if fileSize < 0 {
		return nil, fmt.Errorf("negative file size %d", fileSize)
	}
	var boxes []Box
	var offset int64
	hdr := make([]byte, largeHeaderSize)

	for offset < fileSize {
		remaining := fileSize - offset
		if remaining < normalHeaderSize {
			return boxes, fmt.Errorf("truncated box header at offset %d", offset)
		}

		n := len(hdr)
		if remaining < int64(n) {
			n = int(remaining)
		}
		if _, err := r.ReadAt(hdr[:n], offset); err != nil {
			return boxes, fmt.Errorf("read box header at offset %d: %w", offset, err)
		}

		size32 := binary.BigEndian.Uint32(hdr[0:4])
		boxType := string(hdr[4:8])
		headerSize := int64(normalHeaderSize)

		var boxSize int64
		switch size32 {
		case 0:
			// Size extends to the end of the file.
			boxSize = remaining
		case 1:
			if n < largeHeaderSize {
				return boxes, fmt.Errorf("truncated 64-bit size at offset %d", offset)
			}
			headerSize = largeHeaderSize
			boxSize = int64(binary.BigEndian.Uint64(hdr[8:16])) //nolint:gosec // bounded by file size check below
		default:
			boxSize = int64(size32)
		}

		if boxSize < headerSize {
			return boxes, fmt.Errorf("invalid box size %d for %q at offset %d", boxSize, boxType, offset)
		}
		if offset+boxSize > fileSize {
			// Tolerate a truncated final box; record what is actually there.
			boxSize = remaining
		}

		boxes = append(boxes, Box{
			Type:       boxType,
			Offset:     offset,
			Size:       boxSize,
			HeaderSize: headerSize,
		})
		offset += boxSize
	}
	return boxes, nil
}

// ReadFtyp reads the major brand and the list of compatible brands from an
// ftyp box. The payload is small (tens of bytes), so it is read in full.
func ReadFtyp(r io.ReaderAt, b Box) (major string, compatible []string, err error) {
	payloadSize := b.Size - b.HeaderSize
	if payloadSize < 8 {
		return "", nil, fmt.Errorf("ftyp payload too small: %d bytes", payloadSize)
	}
	buf := make([]byte, payloadSize)
	if _, err := r.ReadAt(buf, b.Offset+b.HeaderSize); err != nil {
		return "", nil, fmt.Errorf("read ftyp payload: %w", err)
	}
	major = string(buf[0:4])
	// buf[4:8] is the minor version, which we do not currently expose.
	for off := 8; off+4 <= len(buf); off += 4 {
		compatible = append(compatible, string(buf[off:off+4]))
	}
	return major, compatible, nil
}

// Find returns the first box of the given type, or nil.
func Find(boxes []Box, boxType string) *Box {
	for i := range boxes {
		if boxes[i].Type == boxType {
			return &boxes[i]
		}
	}
	return nil
}

// Count returns how many boxes of the given type are present.
func Count(boxes []Box, boxType string) int {
	n := 0
	for i := range boxes {
		if boxes[i].Type == boxType {
			n++
		}
	}
	return n
}
