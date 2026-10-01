// Package classify turns a probe report into a one-line verdict for a file:
// whether it is fine (ok), worth normalizing (needs_work), or unplayable
// (broken), together with plain-language reasons.
//
// The CLI's `scan` command and the GUI batch list both build on this package,
// so the two can never disagree about which files need attention.
package classify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/YLing2024/mp4norm/internal/probe"
)

// Status is the single-word verdict for a file.
type Status string

const (
	// StatusOK means the file already opens and seeks well.
	StatusOK Status = "ok"
	// StatusNeedsWork means a lossless normalize pass would help.
	StatusNeedsWork Status = "needs_work"
	// StatusBroken means the file cannot be normalized (missing/corrupt data).
	StatusBroken Status = "broken"
)

// Reason codes. The codes are stable identifiers shared with the GUI's i18n
// pack; the human-readable text lives in Reason.Text.
const (
	CodeMoovAtEnd      = "moov-at-end"
	CodeMdatFragmented = "mdat-fragmented"
	CodeNotInterleaved = "not-interleaved"
	CodeMoovMissing    = "moov-missing"
	CodeTruncated      = "truncated-box"
	CodeNotMP4         = "not-mp4"
	CodeScanFailed     = "scan-failed"
)

// Reason is one plain-language reason behind a verdict. Count is only set for
// the mdat-fragmented reason; Message carries the original probe text and is
// used as a fallback for codes without dedicated copy.
type Reason struct {
	Code    string `json:"code"`
	Count   int    `json:"count,omitempty"`
	Message string `json:"message,omitempty"`
}

// Verdict is the classification of a single file.
type Verdict struct {
	Path    string        `json:"path"`
	Name    string        `json:"name"`
	Size    int64         `json:"size"`
	Status  Status        `json:"status"`
	Reasons []Reason      `json:"reasons"`
	Probe   *probe.Report `json:"probe,omitempty"`
	Error   string        `json:"error,omitempty"`
}

// NeedsWork reports whether the verdict is the "worth normalizing" kind.
func (v Verdict) NeedsWork() bool { return v.Status == StatusNeedsWork }

// Unreadable records an input path that could not be walked or read.
type Unreadable struct {
	Path string
	Err  error
}

// Collect expands directory arguments into their video files (.mp4/.m4v/.mov)
// and keeps explicit file arguments as-is. Walks are recursive and results are
// de-duplicated case insensitively so overlapping inputs are only counted once.
func Collect(paths []string) (files []string, bad []Unreadable) {
	seen := make(map[string]bool)
	add := func(p string) {
		key := strings.ToLower(filepath.Clean(p))
		if seen[key] {
			return
		}
		seen[key] = true
		files = append(files, p)
	}
	for _, a := range paths {
		fi, err := os.Stat(a)
		if err != nil {
			bad = append(bad, Unreadable{Path: a, Err: err})
			continue
		}
		if !fi.IsDir() {
			add(a)
			continue
		}
		err = filepath.WalkDir(a, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".mp4", ".m4v", ".mov":
				add(path)
			}
			return nil
		})
		if err != nil {
			bad = append(bad, Unreadable{Path: a, Err: err})
		}
	}
	return files, bad
}

// BrokenFromError builds the verdict for a path that could not be collected.
func BrokenFromError(path string, err error) Verdict {
	return Verdict{
		Path:    path,
		Name:    filepath.Base(path),
		Status:  StatusBroken,
		Error:   err.Error(),
		Reasons: []Reason{{Code: CodeScanFailed, Message: err.Error()}},
	}
}

// All classifies the given files in parallel, preserving input order. The
// worker count is capped at four to stay pleasant on mechanical disks.
func All(paths []string) []Verdict {
	const workers = 4
	verdicts := make([]Verdict, len(paths))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, p := range paths {
		i, p := i, p
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			verdicts[i] = ClassifyFile(p)
		}()
	}
	wg.Wait()
	return verdicts
}

// ClassifyFile inspects the file at path and returns its verdict. I/O and
// format failures are reported as StatusBroken rather than as errors, so a
// batch scan can keep going past a bad file.
func ClassifyFile(path string) Verdict {
	v := Verdict{Path: path, Name: filepath.Base(path)}

	fi, err := os.Stat(path)
	if err != nil {
		v.Status = StatusBroken
		v.Error = err.Error()
		v.Reasons = []Reason{{Code: CodeScanFailed, Message: err.Error()}}
		return v
	}
	v.Size = fi.Size()

	rep, err := probe.Analyze(path)
	if err != nil {
		v.Status = StatusBroken
		v.Error = err.Error()
		v.Reasons = []Reason{{Code: CodeNotMP4, Message: err.Error()}}
		return v
	}
	v.Probe = rep
	v.Status, v.Reasons = classifyReport(rep, NotInterleaved(path))
	return v
}

// classifyReport applies the decision rules to an already-computed probe
// report. notInterleaved is the best-effort interleave check (see
// NotInterleaved) and is only consulted for files that are otherwise fine.
func classifyReport(rep *probe.Report, notInterleaved bool) (Status, []Reason) {
	var reasons []Reason
	seen := make(map[string]bool)
	add := func(r Reason) {
		if seen[r.Code] {
			return
		}
		seen[r.Code] = true
		reasons = append(reasons, r)
	}

	// A critical finding — or a truncated final box — makes the file
	// unprocessable; no amount of container rewriting can recover it.
	broken := false
	for _, f := range rep.Findings {
		if f.Severity == probe.SeverityCritical || f.Code == CodeTruncated {
			broken = true
			add(Reason{Code: f.Code, Message: f.Message})
		}
	}
	if broken {
		return StatusBroken, reasons
	}

	// Repairable problems, most important first.
	if rep.MoovPosition == probe.MoovEnd {
		add(Reason{Code: CodeMoovAtEnd})
	}
	if rep.MdatCount > 1 {
		add(Reason{Code: CodeMdatFragmented, Count: rep.MdatCount})
	}
	if notInterleaved {
		add(Reason{Code: CodeNotInterleaved})
	}
	// Any remaining warning (e.g. no-ftyp) is also repairable. moov-at-end is
	// already added above and dedupes here.
	for _, f := range rep.Findings {
		if f.Severity == probe.SeverityWarn {
			add(Reason{Code: f.Code, Message: f.Message})
		}
	}

	if len(reasons) > 0 {
		return StatusNeedsWork, reasons
	}
	return StatusOK, nil
}

// Text renders a reason as one plain-language sentence. Unknown codes fall
// back to the probe's own message so a reason is never rendered empty.
func (r Reason) Text(lang string) string {
	en := lang == "en"
	switch r.Code {
	case CodeMoovAtEnd:
		if en {
			return "Index at the end: players must read the whole file before the first frame"
		}
		return "索引在文件尾部：播放器要读完整文件才能出画面，也不能边下边播"
	case CodeMdatFragmented:
		if en {
			return fmt.Sprintf("Fragmented into %d data blocks: seeking jumps back and forth", r.Count)
		}
		return fmt.Sprintf("数据碎片化（%d 个数据块）：拖动进度条时播放器要在文件里来回跳", r.Count)
	case CodeNotInterleaved:
		if en {
			return "Audio and video are not interleaved"
		}
		return "音视频未交织：声音和画面数据分开存，拖动卡顿"
	case CodeMoovMissing:
		if en {
			return "No index (moov) found; the file is probably damaged"
		}
		return "未找到索引（moov 缺失），文件可能损坏"
	case CodeTruncated:
		if en {
			return "File is truncated: the final data block is incomplete"
		}
		return "文件被截断：最后一个数据块不完整，可能没下载完"
	case CodeNotMP4:
		if en {
			return "Not a valid MP4 file"
		}
		return "不是有效的 MP4 文件"
	case CodeScanFailed:
		if en {
			return "Could not read the file"
		}
		return "无法读取文件"
	case "no-ftyp":
		if en {
			return "No ftyp box found; the file type is undeclared"
		}
		return "未找到 ftyp 块：文件类型未声明"
	case "fragmented":
		if en {
			return "Fragmented MP4 (moof boxes present)"
		}
		return "分片 MP4（存在 moof 块）"
	}
	if r.Message != "" {
		return r.Message
	}
	return r.Code
}

// ReasonsText joins every reason behind a verdict with " + ". A clean file
// gets the "already normalized" copy instead of an empty string.
func (v Verdict) ReasonsText(lang string) string {
	if len(v.Reasons) == 0 {
		if lang == "en" {
			return "Already normalized"
		}
		return "已规范"
	}
	out := ""
	for i, r := range v.Reasons {
		if i > 0 {
			out += " + "
		}
		out += r.Text(lang)
	}
	return out
}
