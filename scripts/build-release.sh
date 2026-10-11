#!/usr/bin/env bash
#
# Build the agent for every platform it is released for, and a deb and an rpm
# for each Linux architecture.
#
#   ./scripts/build-release.sh 0.2.4 dist     stamp 0.2.4, write archives to dist/
#   ./scripts/build-release.sh dev dist       unstamped, as CI builds it
#
# One script for CI and for the release, so a tag is never the first time a
# target is built. Each target is a static binary (CGO off) in an archive with
# the licence and README, and SHA256SUMS lists them all.
#
set -euo pipefail

cd "$(dirname "$0")/.."

version="${1:?usage: build-release.sh VERSION OUTDIR}"
out="${2:?usage: build-release.sh VERSION OUTDIR}"

# os/arch[/goarm]
targets=(
  linux/amd64 linux/arm64 linux/arm/7 linux/386
  windows/amd64 windows/arm64
  darwin/amd64 darwin/arm64
  freebsd/amd64
)

# An unstamped build keeps the -dev default compiled into the source, rather
# than claiming a release number it is not.
ldflags="-s -w"
if [ "$version" != dev ]; then
  ldflags="$ldflags -X github.com/certpilot/certpilot-agent.Version=$version"
fi

# Pinned, and built rather than downloaded, so the packages come from a known
# version of the tool on any machine with Go.
nfpm_version=v2.46.3

rm -rf "$out"
mkdir -p "$out"
out="$(cd "$out" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

GOBIN="$work/bin" go install "github.com/goreleaser/nfpm/v2/cmd/nfpm@$nfpm_version"

# A package version has to start with a digit; an unstamped build sorts below
# every release.
pkg_version="$version"
[ "$version" = dev ] && pkg_version=0.0.0-dev

for target in "${targets[@]}"; do
  IFS=/ read -r goos goarch goarm <<<"$target"
  name="certpilot-agent_${version}_${goos}_${goarch}${goarm:+v$goarm}"
  bin=certpilot-agent
  [ "$goos" = windows ] && bin=certpilot-agent.exe

  dir="$work/$name"
  mkdir -p "$dir"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOARM="${goarm:-}" \
    go build -trimpath -ldflags="$ldflags" -o "$dir/$bin" ./cmd/
  cp LICENSE README.md "$dir/"

  if [ "$goos" = windows ]; then
    (cd "$work" && zip -qr "$out/$name.zip" "$name")
  else
    tar -C "$work" -czf "$out/$name.tar.gz" "$name"
  fi
  if [ "$goos" = linux ]; then
    sed "s|@BINARY@|$dir/$bin|" packaging/nfpm.yaml > "$work/nfpm.yaml"
    for format in deb rpm; do
      NFPM_ARCH="$goarch${goarm:-}" NFPM_VERSION="$pkg_version" \
        "$work/bin/nfpm" package --config "$work/nfpm.yaml" --packager "$format" --target "$out/" >/dev/null
    done
  fi
  echo "built $name"
done

sum() { if command -v sha256sum >/dev/null; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }
(cd "$out" && sum certpilot-agent[_-]* > SHA256SUMS)
echo "wrote $(ls "$out" | wc -l | tr -d ' ') files to $out"
