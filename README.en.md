[English](README.en.md) | [简体中文](README.md)

# mp4norm

Cross-platform **MP4 normalization** — make MP4 files open instantly and seek fast, by fixing the container layout instead of re-encoding.

## The problem

Many MP4 files are "weirdly packaged" in ways that are invisible until you try to play them:

- the `moov` (index) atom sits at the **end** of the file → progressive/streaming playback has to download almost the whole file before the first frame;
- audio and video samples are **not interleaved** → seeking makes the player jump back and forth across the file;
- sparse/irregular keyframes, broken `elst` edit lists or inconsistent timescales → slow or broken scrubbing.

The codec data is usually fine. It is the **container layout** that needs fixing.

## What it does

`mp4norm` rewrites the container **losslessly** (stream copy, no quality loss):

1. Move `moov` to the front (**faststart**).
2. Re-interleave audio/video into small time buckets (~0.5–1.0 s).
3. Clean up edit lists / timescale inconsistencies and recompute chunk offsets (`stco`/`co64`).
4. Output either a **progressive faststart MP4** or a **fragmented MP4 (fMP4) with `sidx`**, selectable.
5. Validate the result (moov position, interleave delta, keyframe interval).

`probe` reports graded findings — `critical` / `warn` / `info` — such as `moov-at-end` (index at the end), `fragmented` (fragmented file), `no-ftyp` (file type undeclared), and a `truncated-box` warning for a truncated tail or trailing bytes.

When the source cannot be fixed by container surgery (very sparse keyframes, non-standard encoding, VFR), an **optional re-encode** path delegates to `ffmpeg`, preferring hardware encoders (NVENC / QSV / AMF / VideoToolbox).

## Stack

| Layer | Choice |
| --- | --- |
| Language | Go |
| Container engine | [`Eyevinn/mp4ff`](https://github.com/Eyevinn/mp4ff) (MIT) |
| Re-encode (optional) | `ffmpeg` subprocess, hardware encoders preferred |
| GUI | [Wails](https://wails.io) (Go backend + web frontend) |
| Platforms | Windows, macOS, Linux |

## Performance principles

- Lossless path is **I/O-bound**: single pass, large buffers / mmap, never load the whole file into memory.
- No temp-file shuffling on the lossless path.
- Concurrency across files.
- Hardware encoders for the re-encode path.

## Measured on real files

Test environment: Windows 11 · Go 1.27 · H: drive (7200rpm-class mechanical disk) · measured 2026-10-01

| Scenario | Result |
| --- | --- |
| Normalizing a real 1.2 GB video (index at the end) | **15.9 s** |
| Same file, "replace the original" mode (with backup) | **23.7 s** |
| Peak memory while normalizing the 1.2 GB file | **67 MB** (the whole file is never read into memory) |
| Bytes that must be read before the first playable frame | **1.2 GiB → 5.7 MiB** |
| Scanning 30 real videos (191 MB ~ 1811 MB) | **0.3 s** (only the file header and footer are read) |
| A 40 KB small file | Instant (milliseconds) |

For comparison: on the same machine, `ffmpeg -c copy +faststart` on an 800 MB
oddly-muxed file takes about 18 s to write and another ~36 s to reorder moov
(~54 s total) — mp4norm takes the container-surgery path and handles 1.2 GB in
about 16 s.

## Usage

CLI:

```
mp4norm probe <file>                 # diagnose the container layout
mp4norm normalize <input>            # faststart + interleave (lossless)
mp4norm normalize -format fmp4 <in>  # fragmented MP4 with sidx (lossless)
mp4norm faststart <input>            # only move moov to the front
mp4norm reencode -crf 23 <input>     # optional re-encode (fix sparse keyframes / VFR)
mp4norm tmp <dir...>                 # list leftover temp files (`.mp4norm-tmp-*`) and their sizes
```

Common flags: `-o <out>` output path, `-window <ms>` interleave window,
`-frag-ms <ms>` fragment duration, `-vcodec h264|h265|copy`, `-hw auto|on|off`,
`-gop <sec>` keyframe interval.

If the process is killed mid-rewrite (Task Manager end task, forced window
close), it can leave a `.mp4norm-tmp-*` file behind. Before each rewrite the
target directory's residue older than **60 minutes** is swept automatically and
logged (a concurrently running instance's temp file is fresh and never swept);
`mp4norm tmp <dir>` shows what is currently there.

Batch detection (the recommended entry point) — scan a folder and get a
plain-language verdict per file:

```
mp4norm scan <dir|file...>         # human-readable table
mp4norm scan <dir> --needs-work    # only the files that need work
mp4norm scan <dir> --json          # machine-readable
mp4norm scan <dir> -v              # include raw fields (brand / moov position / mdat boxes)
```

Three verdicts only:

- ✅ **no work needed** — index at the front, data blocks intact, nothing odd
- ⚠️ **needs work** — index at the end, fragmented into many data blocks, or audio/video not interleaved
- ❌ **cannot process** — missing index or corrupt file

Exit codes: `0` nothing to do / `1` some files need work / `2` some files are broken — convenient for scripting.

`batch` normalizes many files or directories in parallel:

``` 
mp4norm batch [-jobs n] [-format progressive|fmp4] [-outdir dir] [-suffix s] <input...|dir...>
```

`watch` polls folders and normalizes new files once they have stopped being written:

```
mp4norm watch [-interval 5m] [-outdir dir] [-in-place] [-backup-dir dir] [-once] [-quiet] <dir...>
```

The **first round is a baseline observation pass**: every candidate file
(`.mp4`/`.m4v`/`.mov`) is recorded with its size and modification time and left
alone, so a download or recording that is still in progress is never mistaken
for a finished file. Classification and processing start from the second round.
A file that is still changing is reported as `writing` and is never recorded as
failed — only a file that has settled and is genuinely broken is. `-once` runs a
single observation round and exits with scan-style codes (`0`/`1`/`2`); run it
twice to observe, then process.

Desktop GUI (Wails) — **batch detection and normalization are the main entry point**:

```
cd gui && wails build                        # produces build/bin/mp4norm
./build/bin/mp4norm.exe "D:\your\video\dir"  # or pass dirs/files on the command line
```

- "Choose folder…" or "Choose files…" (multi-select), or **drag files/folders straight into the window** → the list shows a **verdict and reason per file**, filterable by verdict and sortable by size
- "Fix N file(s)" processes every file that needs work, showing `Fixing N/M: filename` plus per-file status, ending with an ok / skipped / failed summary; failed rows offer **Retry**, and the verdicts and button count refresh afterwards
- Raw fields (brand / moov position / mdat boxes) live in each row's details instead of the main view
- The single-file flow (pick one file → inspect → normalize / re-encode) is collapsed into a "Single file" row until you open it
- Re-encoding is an optional advanced path: **usually unnecessary**, only for slow seeking or odd frame rates; it re-encodes and is lossy

### GUI language (bilingual)

The GUI defaults to **Simplified Chinese**. A `中文 / English` switch sits in the
top-right corner of the window and applies immediately; the **choice is
remembered** (stored in browser local storage `localStorage`, key
`mp4norm.lang`) and restored on the next launch. The native open/save dialog
titles and the explanatory text for `probe` findings follow the active
language too. Every parameter carries a single-line hint, with the full
explanation tucked into the **ⓘ** popover next to the label (shown on hover or
click).

## Bundling ffmpeg

The optional re-encode path uses an external ffmpeg. `mp4norm` looks for one in
order: `MP4NORM_FFMPEG`, a binary bundled next to the executable or in the
working directory (`ffmpeg/` or `third_party/ffmpeg/`), then `PATH`.

Fetch a static build into `third_party/ffmpeg/bin`:

```
make fetch-ffmpeg   # or scripts/fetch-ffmpeg.ps1 / scripts/fetch-ffmpeg.sh
```

Note: the fetched builds are GPL. Redistributing them puts the bundle under GPL
terms; ship an LGPL build or require a user-provided ffmpeg if that matters.

## Development

```
make build   # CLI -> bin/mp4norm
make test    # go test ./...
make vet
make fmt
make bench   # lossless-path benchmarks
make gui     # Wails desktop app -> gui/build/bin
```

CI (`.github/workflows/ci.yml`) runs vet, tests and a CLI build on Windows,
macOS and Linux, and builds the GUI on all three. Tagging `v*` publishes CLI
binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64 and
windows/amd64.

## Status

Working: probe, lossless faststart, interleaving, fMP4 + sidx, optional ffmpeg
re-encode with hardware-encoder detection, parallel batch CLI, and a Wails
desktop UI (switchable between Chinese and English) — with cross-platform CI and
a tag-driven release workflow.

## License

MIT — see [LICENSE](LICENSE).

Note: the fetched `ffmpeg` builds are GPL. Redistributing them puts the bundle
under GPL terms; ship an LGPL build or require a user-provided ffmpeg if that
matters.
