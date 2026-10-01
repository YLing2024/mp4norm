package classify

import (
	"strings"
	"testing"

	"github.com/YLing2024/mp4norm/internal/probe"
)

func rep(moov probe.MoovPosition, mdat int, findings ...probe.Finding) *probe.Report {
	return &probe.Report{MoovPosition: moov, MdatCount: mdat, Findings: findings}
}

func hasReason(reasons []Reason, code string) bool {
	for _, r := range reasons {
		if r.Code == code {
			return true
		}
	}
	return false
}

func TestCleanFileIsOK(t *testing.T) {
	status, reasons := classifyReport(rep(probe.MoovFront, 1), false)
	if status != StatusOK {
		t.Fatalf("status = %q, want ok", status)
	}
	if len(reasons) != 0 {
		t.Fatalf("reasons = %+v, want none", reasons)
	}
}

func TestMoovAtEndNeedsWork(t *testing.T) {
	status, reasons := classifyReport(rep(probe.MoovEnd, 1,
		probe.Finding{Severity: probe.SeverityWarn, Code: CodeMoovAtEnd, Message: "raw"}), false)
	if status != StatusNeedsWork {
		t.Fatalf("status = %q, want needs_work", status)
	}
	if !hasReason(reasons, CodeMoovAtEnd) {
		t.Fatalf("reasons = %+v, want moov-at-end", reasons)
	}
}

func TestFragmentedMdatNeedsWorkWithCount(t *testing.T) {
	status, reasons := classifyReport(rep(probe.MoovFront, 4866), false)
	if status != StatusNeedsWork {
		t.Fatalf("status = %q, want needs_work", status)
	}
	var got *Reason
	for i := range reasons {
		if reasons[i].Code == CodeMdatFragmented {
			got = &reasons[i]
		}
	}
	if got == nil {
		t.Fatalf("reasons = %+v, want mdat-fragmented", reasons)
	}
	if got.Count != 4866 {
		t.Fatalf("count = %d, want 4866", got.Count)
	}
	if txt := got.Text("zh"); !strings.Contains(txt, "4866") {
		t.Fatalf("zh text = %q, want it to mention 4866", txt)
	}
}

func TestNotInterleavedNeedsWork(t *testing.T) {
	status, reasons := classifyReport(rep(probe.MoovFront, 1), true)
	if status != StatusNeedsWork {
		t.Fatalf("status = %q, want needs_work", status)
	}
	if !hasReason(reasons, CodeNotInterleaved) {
		t.Fatalf("reasons = %+v, want not-interleaved", reasons)
	}
}

func TestMissingMoovIsBroken(t *testing.T) {
	status, reasons := classifyReport(rep(probe.MoovUnknown, 0,
		probe.Finding{Severity: probe.SeverityCritical, Code: CodeMoovMissing, Message: "raw"}), false)
	if status != StatusBroken {
		t.Fatalf("status = %q, want broken", status)
	}
	if !hasReason(reasons, CodeMoovMissing) {
		t.Fatalf("reasons = %+v, want moov-missing", reasons)
	}
}

func TestTruncatedIsBroken(t *testing.T) {
	status, _ := classifyReport(rep(probe.MoovFront, 1,
		probe.Finding{Severity: probe.SeverityWarn, Code: CodeTruncated, Message: "raw"}), false)
	if status != StatusBroken {
		t.Fatalf("status = %q, want broken", status)
	}
}

func TestOtherWarnNeedsWork(t *testing.T) {
	status, reasons := classifyReport(rep(probe.MoovFront, 1,
		probe.Finding{Severity: probe.SeverityWarn, Code: "no-ftyp", Message: "raw"}), false)
	if status != StatusNeedsWork {
		t.Fatalf("status = %q, want needs_work", status)
	}
	if !hasReason(reasons, "no-ftyp") {
		t.Fatalf("reasons = %+v, want no-ftyp", reasons)
	}
}

func TestReasonTextFallbackAndEnglish(t *testing.T) {
	r := Reason{Code: "custom", Message: "some finding"}
	if got := r.Text("en"); got != "some finding" {
		t.Fatalf("Text(en) = %q, want the message", got)
	}
	if got := (Reason{Code: CodeMoovAtEnd}).Text("en"); !strings.Contains(got, "Index at the end") {
		t.Fatalf("Text(en) = %q", got)
	}
}
