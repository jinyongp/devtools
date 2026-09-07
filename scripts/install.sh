#!/bin/sh
# Parse the complete installer before executing streamed input.
{
set -eu

fail() {
  printf '{"schema_version":1,"ok":false,"error":{"code":"install_failed","message":"%s"}}\n' "$1" >&2
  exit 1
}

usage() {
  printf '%s\n' 'Usage: sh install.sh install|update [--version VERSION] [--source DIRECTORY_OR_HTTPS_URL] [--bin-dir DIRECTORY]'
}

if [ "${1:-}" = '--help' ]; then usage; exit 0; fi
action=${1:-}
case "$action" in install|update) shift ;; *) usage >&2; exit 2 ;; esac
version=
source=
bin_dir=${HOME:?HOME is required}/.local/bin
seen=' '
while [ "$#" -gt 0 ]; do
  option=$1
  case "$option" in --version|--source|--bin-dir) ;; *) fail 'Unknown installer option.' ;; esac
  case "$seen" in *" $option "*) fail 'Specify each option once.' ;; esac
  seen="$seen$option "
  [ "$#" -ge 2 ] || fail 'Option requires a value.'
  [ -n "$2" ] || fail 'Option requires a nonempty value.'
  case "$option" in
    --version) version=$2 ;;
    --source) source=$2 ;;
    --bin-dir) bin_dir=$2 ;;
  esac
  shift 2
done
download() {
  curl --fail --silent --location --proto '=https' --proto-redir '=https' \
    --connect-timeout 15 --max-time 120 "$@" 2>/dev/null
}
if [ -z "$source" ]; then
  release_url=${DEVTOOLS_RELEASE_URL:-https://github.com/jinyongp/devtools/releases}
  case "$release_url" in https://*) ;; *) fail 'Release URL must use HTTPS.' ;; esac
  if [ -z "$version" ]; then
    version=$(download "${release_url%/}/latest/download/version.txt") || fail 'Cannot resolve the latest stable release.'
  fi
  source="${release_url%/}/download/v$version"
fi
case "$version" in ''|.|..|*[!a-zA-Z0-9._-]*) fail 'Specify a valid release version; custom sources require --version.' ;; esac
case "$(uname -s)" in Linux) platform=linux ;; Darwin) platform=darwin ;; *) fail 'Unsupported operating system.' ;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) fail 'Unsupported architecture.' ;; esac

archive="devtools_${version}_${platform}_${arch}.tar.gz"
mkdir -p "$bin_dir" || fail 'Cannot create installation directory.'
bin_dir=$(cd "$bin_dir" && pwd -P)
destination="$bin_dir/devtools"
lock="$bin_dir/.devtools-install.lock"
mkdir "$lock" 2>/dev/null || fail 'Another installation owns the installation lock.'
scratch=
cleanup() {
  if [ -n "$scratch" ]; then rm -rf "$scratch"; fi
  rmdir "$lock"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP
if [ "$action" = install ]; then
  if [ -e "$destination" ] || [ -L "$destination" ]; then fail 'An installation already exists; use update.'; fi
else
  [ -f "$destination" ] && [ ! -L "$destination" ] || fail 'Update requires an existing regular executable.'
fi
scratch=$(mktemp -d "$bin_dir/.devtools-install.XXXXXXXX") || fail 'Cannot prepare installation.'

fetch() {
  case "$source" in
    https://*)
      download "${source%/}/$1" -o "$2" || fail 'Cannot download release artifact.'
      ;;
    *://*) fail 'Remote release sources must use HTTPS.' ;;
    *) cp "${source%/}/$1" "$2" || fail 'Cannot read release artifact.' ;;
  esac
}
fetch "$archive" "$scratch/package.tar.gz"
fetch "$archive.sha256" "$scratch/checksum"
read -r expected checksum_name extra < "$scratch/checksum" || fail 'Cannot read release checksum.'
[ "$checksum_name" = "$archive" ] && [ -z "$extra" ] || fail 'Checksum names a different artifact.'
[ "${#expected}" -eq 64 ] || fail 'Invalid release checksum.'
case "$expected" in *[!0-9a-fA-F]*) fail 'Invalid release checksum.' ;; esac
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$scratch/package.tar.gz" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$scratch/package.tar.gz" | awk '{print $1}')
fi
[ "$actual" = "$expected" ] || fail 'Release checksum mismatch.'
[ "$(tar -tzf "$scratch/package.tar.gz")" = devtools ] || fail 'Release must contain exactly one devtools executable.'
listing=$(tar -tvzf "$scratch/package.tar.gz") || fail 'Cannot inspect release archive.'
case "$listing" in -*) ;; *) fail 'Release executable must be a regular file.' ;; esac
tar -xzf "$scratch/package.tar.gz" -C "$scratch" devtools || fail 'Cannot extract release executable.'
[ -f "$scratch/devtools" ] && [ ! -L "$scratch/devtools" ] || fail 'Invalid release executable.'
chmod 0755 "$scratch/devtools" || fail 'Cannot set executable permissions.'
"$scratch/devtools" version > "$scratch/version.json" || fail 'Release executable cannot run on this system.'
grep -F '"version":"'"$version"'"' "$scratch/version.json" >/dev/null || fail 'Release version does not match the request.'
mv -f "$scratch/devtools" "$destination" || fail 'Cannot publish installed executable.'
printf '{"schema_version":1,"ok":true,"data":{"action":"%s","version":"%s"}}\n' "$action" "$version"
}
