// Package probe analyses an MP4 file's container layout and reports why it may
// open slowly or seek badly. It never decodes media and only reads small box
// headers, so it is safe to run on multi-gigabyte files.
package probe

import (
	"fmt"
	"os"
	"strings"

	"github.com/YLing2024/mp4norm/internal/isobmff"
)

// Severity classifies a finding.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarn     Severity = "warn"
	SeverityCritical Severity = "critical"
)

// MoovPosition describes where the movie header sits relative to the media data.
type MoovPosition string

const (
	MoovFront   MoovPosition = "front"
	MoovEnd     MoovPosition = "end"
	MoovUnknown MoovPosition = "unknown"
)

// Finding is a single diagnostic observation.
type Finding struct {
	Severity Severity
	Code     string
	Message  string
}

// Report is the result of analysing one file.
type Report struct {
	Path         string
	FileSize     int64
	Boxes        []isobmff.Box
	MajorBrand   string
	Compatible   []string
	Fragmented   bool
	MoovPosition MoovPosition
	MdatCount    int
	Findings     []Finding
}

// Analyze opens and inspects the file at path.
func Analyze(path string) (*Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}

	boxes, trunc, err := isobmff.ScanTopLevel(f, fi.Size())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// The only thing that makes a file "not a valid MP4/ISO BMFF file" is an
	// absent or misplaced start: ISO BMFF requires the file to begin with a
	// file-type box (ftyp or styp). A malformed or truncated tail is tolerated
	// above and reported as a finding instead.
	if len(boxes) == 0 {
		return nil, fmt.Errorf("%s: not a valid MP4/ISO BMFF file: no top-level boxes", path)
	}
	if first := boxes[0].Type; first != "ftyp" && first != "styp" {
		return nil, fmt.Errorf("%s: not a valid MP4/ISO BMFF file: first box is %q, expected ftyp or styp", path, first)
	}

	rep := &Report{
		Path:     path,
		FileSize: fi.Size(),
		Boxes:    boxes,
	}
	if ft := isobmff.Find(boxes, "ftyp"); ft != nil {
		if major, compatible, err := isobmff.ReadFtyp(f, *ft); err == nil {
			rep.MajorBrand = major
			rep.Compatible = compatible
		}
	}
	if trunc != nil {
		rep.add(SeverityWarn, "truncated-box", fmt.Sprintf(
			"final box %q at offset %d declares %d bytes but only %d remain — file looks truncated or has trailing bytes",
			trunc.Type, trunc.Offset, trunc.Declared, trunc.Remaining))
	}
	rep.analyse()
	return rep, nil
}

func (r *Report) analyse() {
	moov := isobmff.Find(r.Boxes, "moov")
	mdat := isobmff.Find(r.Boxes, "mdat")
	r.MdatCount = isobmff.Count(r.Boxes, "mdat")
	r.Fragmented = isobmff.Count(r.Boxes, "moof") > 0

	switch {
	case moov == nil:
		r.MoovPosition = MoovUnknown
		r.add(SeverityCritical, "moov-missing",
			"no moov atom found; the file's metadata is missing and it is not playable")
	case mdat == nil:
		r.MoovPosition = MoovUnknown
	case moov.Offset < mdat.Offset:
		r.MoovPosition = MoovFront
	default:
		r.MoovPosition = MoovEnd
		r.add(SeverityWarn, "moov-at-end",
			"moov is stored after mdat (no faststart); progressive and streaming playback must read to the end of the file before the first frame")
	}

	if r.Fragmented {
		r.add(SeverityInfo, "fragmented",
			"fragmented MP4 (moof boxes present); seeking depends on sidx support in the player")
	}

	if isobmff.Find(r.Boxes, "ftyp") == nil {
		r.add(SeverityWarn, "no-ftyp", "no ftyp box found; file type is undeclared")
	}
}

func (r *Report) add(sev Severity, code, msg string) {
	r.Findings = append(r.Findings, Finding{Severity: sev, Code: code, Message: msg})
}

// Summary returns a short human-readable one-line summary of the layout.
func (r *Report) Summary() string {
	return fmt.Sprintf("%s  size=%s  brand=%s  moov=%s  fragmented=%t  mdat=%d",
		r.Path, humanBytes(r.FileSize), orUnknown(r.MajorBrand),
		r.MoovPosition, r.Fragmented, r.MdatCount)
}

// String renders a full report for the terminal.
func (r *Report) String() string {
	var b strings.Builder
	b.WriteString(r.Summary())
	b.WriteString("\n\nBoxes:\n")
	for _, bx := range r.Boxes {
		fmt.Fprintf(&b, "  %-6s offset=%-14d size=%-14d\n", bx.Type, bx.Offset, bx.Size)
	}
	if len(r.Findings) > 0 {
		b.WriteString("\nFindings:\n")
		for _, f := range r.Findings {
			fmt.Fprintf(&b, "  [%s] %s: %s\n", f.Severity, f.Code, f.Message)
		}
	}
	return b.String()
}

// NeedsNormalization reports whether the container has a known problem that
// normalization can fix without re-encoding.
func (r *Report) NeedsNormalization() bool {
	if r.MoovPosition == MoovEnd {
		return true
	}
	if r.Fragmented {
		return true
	}
	return false
}

func orUnknown(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

// HumanBytes formats a byte count with a magnitude-appropriate binary unit and
// one decimal place (e.g. "781.1 MiB"). It is exported so the CLI scan table
// and other callers render sizes exactly like the probe report does.
func HumanBytes(n int64) string { return humanBytes(n) }

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
