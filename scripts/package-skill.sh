#!/bin/sh
set -eu

: "${VERSION:?Set VERSION to the release version}"
case "$VERSION" in ''|.|..|*[!a-zA-Z0-9._-]*) echo 'Invalid VERSION' >&2; exit 2 ;; esac

output=${OUTPUT_DIR:-dist}
mkdir -p "$output"
scratch=$(mktemp -d "${TMPDIR:-/tmp}/devtools-skill-package.XXXXXXXX")
trap 'rm -rf "$scratch"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

archive="devtools-skill_${VERSION}.tar.gz"
go run ./scripts/package-skill \
  --source skills/devtools/SKILL.md \
  --output "$scratch/$archive"
(
  cd "$scratch"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$archive" > "$archive.sha256"
  else
    shasum -a 256 "$archive" > "$archive.sha256"
  fi
)
mv "$scratch/$archive" "$output/$archive"
mv "$scratch/$archive.sha256" "$output/$archive.sha256"
printf '%s\n' "$output/$archive"
