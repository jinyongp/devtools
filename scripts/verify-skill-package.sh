#!/bin/sh
set -eu

[ "$#" -eq 2 ] || {
  echo 'Usage: verify-skill-package.sh ARCHIVE CHECKSUM' >&2
  exit 2
}

archive=$1
checksum=$2
[ -f "$archive" ] && [ ! -L "$archive" ] || {
  echo 'Skill archive is missing or invalid.' >&2
  exit 1
}
[ -f "$checksum" ] && [ ! -L "$checksum" ] || {
  echo 'Skill checksum is missing or invalid.' >&2
  exit 1
}

archive_dir=$(CDPATH='' cd -- "$(dirname -- "$archive")" && pwd)
archive_name=$(basename -- "$archive")
checksum_path=$(CDPATH='' cd -- "$(dirname -- "$checksum")" && pwd)/$(basename -- "$checksum")
read -r expected checksum_name extra < "$checksum_path" || {
  echo 'Cannot read the skill checksum.' >&2
  exit 1
}
[ "$checksum_name" = "$archive_name" ] && [ -z "$extra" ] || {
  echo 'Skill checksum names a different artifact.' >&2
  exit 1
}
[ "${#expected}" -eq 64 ] || {
  echo 'Invalid skill checksum.' >&2
  exit 1
}
case "$expected" in *[!0-9a-fA-F]*) echo 'Invalid skill checksum.' >&2; exit 1 ;; esac

if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$archive_dir/$archive_name" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$archive_dir/$archive_name" | awk '{print $1}')
fi
[ "$actual" = "$expected" ] || {
  echo 'Skill checksum mismatch.' >&2
  exit 1
}

listing=$(tar -tzf "$archive_dir/$archive_name") || {
  echo 'Cannot inspect the skill archive.' >&2
  exit 1
}
expected_listing=$(printf 'devtools/\ndevtools/SKILL.md')
[ "$listing" = "$expected_listing" ] || {
  echo 'Skill archive has an unexpected structure.' >&2
  exit 1
}
member_types=$(tar -tvzf "$archive_dir/$archive_name" | awk '{print substr($1, 1, 1)}') || {
  echo 'Cannot inspect skill archive member types.' >&2
  exit 1
}
expected_types=$(printf 'd\n-')
[ "$member_types" = "$expected_types" ] || {
  echo 'Skill archive contains a non-regular member.' >&2
  exit 1
}

scratch=$(mktemp -d "${TMPDIR:-/tmp}/devtools-skill-verify.XXXXXXXX")
trap 'rm -rf "$scratch"' EXIT
tar -xzf "$archive_dir/$archive_name" -C "$scratch" || {
  echo 'Cannot extract the skill archive.' >&2
  exit 1
}
[ -f "$scratch/devtools/SKILL.md" ] && [ ! -L "$scratch/devtools/SKILL.md" ] || {
  echo 'Skill archive does not contain a regular SKILL.md.' >&2
  exit 1
}
cmp skills/devtools/SKILL.md "$scratch/devtools/SKILL.md" >/dev/null || {
  echo 'Packaged SKILL.md differs from the canonical source.' >&2
  exit 1
}
uvx --from skills-ref==0.1.1 agentskills validate "$scratch/devtools"
