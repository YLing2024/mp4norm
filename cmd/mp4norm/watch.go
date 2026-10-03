package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YLing2024/mp4norm/internal/classify"
	"github.com/YLing2024/mp4norm/internal/normalize"
	"github.com/YLing2024/mp4norm/internal/safefile"
	"github.com/YLing2024/mp4norm/internal/watch"
)

// minWatchInterval is the smallest polling interval the watch command accepts.
// A shorter one would spin on large download folders for no benefit.
const minWatchInterval = 30 * time.Second

// watchSuffix is appended to a file's base name when watch writes a new file
// beside its input.
const watchSuffix = ".norm.mp4"

// runWatch implements `mp4norm watch`: it polls the given directories, waits
// until each file has stopped being written, and normalizes the ones that need
// work. The first round over a folder is a baseline observation pass: every
// file is recorded and left alone so a file that is still being written is not
// mistaken for a finished one. With -once it performs a single observation
// round and exits with scan-style codes (0 all clean / 1 something processed /
// 2 an error); run it twice to observe then process.
func runWatch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	interval := fs.Duration("interval", 5*time.Minute, "轮询间隔（最小 30s）")
	outdir := fs.String("outdir", "", "输出目录（默认：<输入>.norm.mp4 放在每个文件旁边）")
	inPlace := fs.Bool("in-place", false, "备份后替换原文件")
	backupDir := fs.String("backup-dir", "", "就地备份的存放目录（默认：<输入所在目录>/.mp4norm-backup）")
	once := fs.Bool("once", false, "只跑一轮观察后退出（先观察、再处理需运行两次）")
	quiet := fs.Bool("quiet", false, "只报告已处理或出错的文件")
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	roots := fs.Args()
	if len(roots) == 0 {
		return fmt.Errorf("watch: missing <directory...>")
	}
	if *inPlace && *outdir != "" {
		return fmt.Errorf("watch: -in-place and -outdir are mutually exclusive")
	}
	if !*inPlace && *backupDir != "" {
		return fmt.Errorf("watch: -backup-dir requires -in-place")
	}
	if !*once && *interval < minWatchInterval {
		return fmt.Errorf("watch: -interval must be at least %s", minWatchInterval)
	}
	for _, root := range roots {
		fi, err := os.Stat(root)
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return fmt.Errorf("watch: %s is not a directory", root)
		}
	}
	if *outdir != "" {
		if err := os.MkdirAll(*outdir, 0o755); err != nil {
			return err
		}
	}

	transform := func(src normalize.ReadSeekerAt, dst io.Writer) (*normalize.Result, error) {
		return normalize.Interleave(src, dst, normalize.InterleaveOptions{})
	}
	process := func(path string) (string, error) {
		spec := transformSpec{inPlace: *inPlace, backupDir: *backupDir}
		if !*inPlace {
			spec.output = watchOutput(path, *outdir)
		}
		oc, err := safefile.Transform(safefile.Request{
			Input:     path,
			Output:    spec.output,
			InPlace:   spec.inPlace,
			BackupDir: spec.backupDir,
			Transform: transform,
			Logf:      cleanupLog,
		})
		if err != nil {
			return "", err
		}
		printOutcome(oc, false)
		return oc.Output, nil
	}

	targets := make([]watchTarget, len(roots))
	for i, root := range roots {
		st, warn := watch.LoadState(watch.StatePath(root))
		if warn != nil {
			fmt.Fprintf(os.Stderr, "mp4norm: warning: %v\n", warn)
		}
		targets[i] = watchTarget{root: root, state: st}
	}

	runRound := func() (watch.Summary, error) {
		var total watch.Summary
		for i := range targets {
			r := &watch.Runner{
				Root:     targets[i].root,
				State:    targets[i].state,
				Classify: classify.ClassifyFile,
				Process:  process,
				Quiet:    *quiet,
				Log: func(format string, args ...any) {
					fmt.Fprintf(os.Stdout, format+"\n", args...)
				},
				Warn: func(format string, args ...any) {
					fmt.Fprintf(os.Stderr, "mp4norm: "+format+"\n", args...)
				},
			}
			s := r.Pass()
			if err := targets[i].state.Save(watch.StatePath(targets[i].root)); err != nil {
				return total, err
			}
			addSummary(&total, s)
		}
		printWatchSummary(os.Stdout, total, *quiet)
		return total, nil
	}

	if *once {
		// A single round is a pure observation pass. Every file seen for
		// the first time is recorded as writing and skipped; a second run
		// settles the file and only then classifies and processes it.
		total, err := runRound()
		if err != nil {
			return err
		}
		if code := watchExitCode(total); code != 0 {
			return exitCodeError{code: code}
		}
		return nil
	}

	for {
		if _, err := runRound(); err != nil {
			return err
		}
		time.Sleep(*interval)
	}
}

// watchTarget pairs a scanned directory with its persisted state.
type watchTarget struct {
	root  string
	state *watch.State
}

// watchOutput resolves where a normalized copy of path is written.
func watchOutput(path, outdir string) string {
	if outdir == "" {
		return outputPath("", path, watchSuffix)
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + watchSuffix
	return filepath.Join(outdir, base)
}

// addSummary accumulates one root's pass into the run total.
func addSummary(total *watch.Summary, s watch.Summary) {
	total.Scanned += s.Scanned
	total.NeedsWork += s.NeedsWork
	total.Processed += s.Processed
	total.Failed += s.Failed
	total.Writing += s.Writing
	total.Skipped += s.Skipped
}

// printWatchSummary writes the one-line end-of-round summary. In quiet mode it
// stays silent unless something was processed or went wrong.
func printWatchSummary(w io.Writer, s watch.Summary, quiet bool) {
	if quiet && s.Processed == 0 && s.Failed == 0 {
		return
	}
	fmt.Fprintf(w, "watch: scanned %d, needs work %d, processed %d, failed %d, writing %d\n",
		s.Scanned, s.NeedsWork, s.Processed, s.Failed, s.Writing)
}

// watchExitCode maps a round's tally onto scan's exit-code meanings.
func watchExitCode(s watch.Summary) int {
	if s.Failed > 0 {
		return 2
	}
	if s.Processed > 0 {
		return 1
	}
	return 0
}
