package main

import (
	"flag"
	"reflect"
	"testing"
)

func TestReorderArgsMovesFlagsBeforePositionals(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("o", "", "")
	fs.Int("window", 0, "")

	got := reorderArgs(fs, []string{"in.mp4", "-o", "out.mp4", "-window", "500"})
	want := []string{"-o", "out.mp4", "-window", "500", "in.mp4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reorderArgs = %v, want %v", got, want)
	}
	if err := fs.Parse(got); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if fs.NArg() != 1 || fs.Arg(0) != "in.mp4" {
		t.Fatalf("NArg=%d args=%v, want [in.mp4]", fs.NArg(), fs.Args())
	}
	if got := fs.Lookup("o").Value.String(); got != "out.mp4" {
		t.Fatalf("-o = %q, want out.mp4", got)
	}
}

func TestReorderArgsInlineValue(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("o", "", "")

	got := reorderArgs(fs, []string{"in.mp4", "-o=out.mp4"})
	want := []string{"-o=out.mp4", "in.mp4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reorderArgs = %v, want %v", got, want)
	}
}

func TestRequireOneArgRejectsExtras(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse([]string{"a.mp4", "b.mp4"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, err := requireOneArg(fs, "normalize"); err == nil {
		t.Fatal("expected error for extra positional arguments, got nil")
	}
}

func TestRequireOneArgAcceptsSingle(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse([]string{"a.mp4"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := requireOneArg(fs, "normalize")
	if err != nil {
		t.Fatalf("requireOneArg: %v", err)
	}
	if got != "a.mp4" {
		t.Fatalf("arg = %q, want a.mp4", got)
	}
}

func TestBuildSpecRejectsInPlaceWithOutput(t *testing.T) {
	if _, err := buildSpec("normalize", "in.mp4", "out.mp4", ".norm.mp4", true, ""); err == nil {
		t.Fatal("buildSpec = nil, want a mutual-exclusion error")
	}
}

func TestBuildSpecRejectsBackupDirWithoutInPlace(t *testing.T) {
	if _, err := buildSpec("normalize", "in.mp4", "", ".norm.mp4", false, "/tmp/bk"); err == nil {
		t.Fatal("buildSpec = nil, want a backup-dir error")
	}
}

func TestBuildSpecResolvesNewFileOutput(t *testing.T) {
	spec, err := buildSpec("normalize", "in.mp4", "", ".norm.mp4", false, "")
	if err != nil {
		t.Fatalf("buildSpec: %v", err)
	}
	if spec.inPlace || spec.output != "in.norm.mp4" {
		t.Fatalf("spec = %+v, want new-file output in.norm.mp4", spec)
	}
}
