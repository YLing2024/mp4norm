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

## Usage

CLI:

```
mp4norm probe <file>                 # diagnose the container layout
mp4norm normalize <input>            # faststart + interleave (lossless)
mp4norm normalize -format fmp4 <in>  # fragmented MP4 with sidx (lossless)
mp4norm faststart <input>            # only move moov to the front
mp4norm reencode -crf 23 <input>     # optional re-encode (fix sparse keyframes / VFR)
```

Common flags: `-o <out>` output path, `-window <ms>` interleave window,
`-frag-ms <ms>` fragment duration, `-vcodec h264|h265|copy`, `-hw auto|on|off`,
`-gop <sec>` keyframe interval.

Desktop GUI (Wails):

```
cd gui && wails build     # produces build/bin/mp4norm
```

## Status

Working: probe, lossless faststart, interleaving, fMP4 + sidx output, and the
optional ffmpeg re-encode path (with hardware encoder detection). A Wails
desktop UI wraps all of it. Remaining: bundling an ffmpeg build for distribution
and CI packaging for the three platforms.

## License

TBD (note: bundling a GPL `ffmpeg` build affects distribution terms).
