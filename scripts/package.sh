#!/bin/sh
set -eu

: "${VERSION:?Set VERSION to the release version}"
case "$VERSION" in ''|.|..|*[!a-zA-Z0-9._-]*) echo 'Invalid VERSION' >&2; exit 2 ;; esac
target_os=${TARGET_OS:-$(go env GOOS)}
target_arch=${TARGET_ARCH:-$(go env GOARCH)}
case "$target_os/$target_arch" in linux/amd64|linux/arm64|darwin/amd64|darwin/arm64) ;; *) echo 'Unsupported target' >&2; exit 2 ;; esac
commit=${COMMIT:-unknown}
case "$commit" in ''|*[!a-zA-Z0-9._-]*) echo 'Invalid COMMIT' >&2; exit 2 ;; esac
output=${OUTPUT_DIR:-dist}
mkdir -p "$output"
scratch=$(mktemp -d "${TMPDIR:-/tmp}/devtools-package.XXXXXXXX")
trap 'rm -rf "$scratch"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP
CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath \
  -ldflags "-s -w -X main.version=$VERSION -X main.commit=$commit" \
  -o "$scratch/devtools" ./cmd/devtools
archive="devtools_${VERSION}_${target_os}_${target_arch}.tar.gz"
tar -czf "$output/$archive" -C "$scratch" devtools
(
  cd "$output"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$archive" > "$archive.sha256"
  else
    shasum -a 256 "$archive" > "$archive.sha256"
  fi
)
printf '%s\n' "$output/$archive"
