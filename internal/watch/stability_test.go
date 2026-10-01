package watch

import "testing"

func TestDecideFirstSeen(t *testing.T) {
	cur := SizeMtime{Size: 10, Mtime: 1}
	if got := Decide(SizeMtime{}, false, cur); got != DecisionFirstSeen {
		t.Fatalf("Decide = %v, want first seen", got)
	}
}

func TestDecideStableWhenUnchanged(t *testing.T) {
	cur := SizeMtime{Size: 10, Mtime: 1}
	if got := Decide(cur, true, cur); got != DecisionStable {
		t.Fatalf("Decide = %v, want stable", got)
	}
}

func TestDecideWritingWhenSizeGrows(t *testing.T) {
	prev := SizeMtime{Size: 10, Mtime: 1}
	cur := SizeMtime{Size: 20, Mtime: 2}
	if got := Decide(prev, true, cur); got != DecisionWriting {
		t.Fatalf("Decide = %v, want writing", got)
	}
}

func TestDecideWritingWhenOnlyMtimeChanges(t *testing.T) {
	prev := SizeMtime{Size: 10, Mtime: 1}
	cur := SizeMtime{Size: 10, Mtime: 2}
	if got := Decide(prev, true, cur); got != DecisionWriting {
		t.Fatalf("Decide = %v, want writing", got)
	}
}
