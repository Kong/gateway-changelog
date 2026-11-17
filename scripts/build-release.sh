#!/usr/bin/env bash
# Cross-compile the changelog CLI and package it as release assets.
#
# Asset names and layout are kept identical to the ones previously produced by
# wangyoucao577/go-release-action, since downstream users download them by name:
#   changelog-<version>-<goos>-<goarch>.tar.gz   (zip on windows)
#   changelog-<version>-<goos>-<goarch>.tar.gz.md5  (hex digest only)
# Each archive holds a single "changelog" ("changelog.exe" on windows) binary.
#
# Usage: scripts/build-release.sh <version> [dist-dir]
# Requires `make generate` to have been run (embeds the markdown template).
set -euo pipefail

version=${1:?usage: build-release.sh <version> [dist-dir]}
dist=${2:-dist}

case "$version" in
  */* | *[[:space:]]* | -*) echo "invalid version: $version" >&2; exit 1 ;;
esac

platforms=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64)

md5_hex() {
  if command -v md5sum >/dev/null 2>&1; then
    md5sum "$1" | cut -d' ' -f1
  else
    md5 -q "$1"
  fi
}

rm -rf "$dist"
mkdir -p "$dist"
dist=$(cd "$dist" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

for platform in "${platforms[@]}"; do
  goos=${platform%/*}
  goarch=${platform#*/}
  name="changelog-${version}-${goos}-${goarch}"
  bin=changelog
  [ "$goos" = windows ] && bin=changelog.exe

  rm -rf "${work:?}"/*
  echo "building $name"
  CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -trimpath -o "$work/$bin" .

  if [ "$goos" = windows ]; then
    asset="$name.zip"
    (cd "$work" && zip -q -X "$dist/$asset" "$bin")
  else
    asset="$name.tar.gz"
    tar -C "$work" -czf "$dist/$asset" "$bin"
  fi
  md5_hex "$dist/$asset" > "$dist/$asset.md5"
done

ls -l "$dist"
