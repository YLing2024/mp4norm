#!/usr/bin/env bash
# Downloads a static ffmpeg build for Linux or macOS into third_party/ffmpeg/bin/.
#
# These builds are GPL. Redistributing them imposes GPL terms on the bundle; if
# that is a problem, ship an LGPL build instead or have users provide their own
# ffmpeg (mp4norm also reads MP4NORM_FFMPEG and PATH).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dest="$root/third_party/ffmpeg/bin"
mkdir -p "$dest"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

case "$(uname -s)" in
  Linux)
    url="https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz"
    echo "downloading $url"
    curl -fL "$url" -o "$tmp/ffmpeg.tar.xz"
    tar -xJf "$tmp/ffmpeg.tar.xz" -C "$tmp"
    cp "$tmp"/ffmpeg-*-static/ffmpeg "$dest/ffmpeg"
    ;;
  Darwin)
    url="https://evermeet.cx/ffmpeg/getrelease/zip"
    echo "downloading $url"
    curl -fL "$url" -o "$tmp/ffmpeg.zip"
    unzip -oq "$tmp/ffmpeg.zip" -d "$tmp"
    cp "$tmp/ffmpeg" "$dest/ffmpeg"
    ;;
  *)
    echo "unsupported OS: $(uname -s)" >&2
    exit 1
    ;;
esac

chmod +x "$dest/ffmpeg"
echo "installed $dest/ffmpeg"
