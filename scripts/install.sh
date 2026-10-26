#!/bin/sh
# Parse the complete installer before executing streamed input.
{
set -eu
installer_stage=input
installer_effects=none

fail() {
  printf '{"schema_version":1,"ok":false,"error":{"code":"install_failed","message":"%s","details":{"operation":"install","phase":"prepare","target":{"kind":"installer","name":"devtools"},"sources":[],"reason":"stage_failed","facts":{"domain":"installer","stage":"%s"},"checks":[],"causes":[],"remedies":[{"argv":[],"required_inputs":[],"message":"Inspect this stage and any retained installation recovery state before retrying.","effect":"read"}],"retry":"inspect_first","effects":"%s"}}}\n' "$1" "$installer_stage" "$installer_effects" >&2
  exit "${2:-1}"
}

usage() {
  printf '%s\n' 'Usage: sh install.sh [install|update] [--version VERSION] [--source DIRECTORY_OR_HTTPS_URL] [--bin-dir DIRECTORY]'
}

if [ "${1:-}" = '--help' ]; then usage; exit 0; fi
action=install
case "${1:-}" in
  '') ;;
  install|update) action=$1; shift ;;
  --*) ;;
  *) fail 'Unknown installer action; use install or update.' 2 ;;
esac
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
  installer_stage=release_lookup
  release_url=${DEVTOOLS_RELEASE_URL:-https://github.com/jinyongp/devtools/releases}
  case "$release_url" in https://*) ;; *) fail 'Release URL must use HTTPS.' ;; esac
  if [ -z "$version" ]; then
    version=$(download "${release_url%/}/latest/download/version.txt") || fail 'Cannot resolve the latest stable release.'
  fi
  source="${release_url%/}/download/v$version"
fi
case "$version" in ''|.|..|*[!a-zA-Z0-9._-]*) fail 'Specify a valid release version; custom sources require --version.' ;; esac
installer_stage=platform
case "$(uname -s)" in Linux) platform=linux ;; Darwin) platform=darwin ;; *) fail 'Unsupported operating system.' ;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) fail 'Unsupported architecture.' ;; esac

archive="devtools_${version}_${platform}_${arch}.tar.gz"
installer_stage=installation_directory
mkdir -p "$bin_dir" 2>/dev/null || fail 'Cannot create installation directory.'
bin_dir=$(CDPATH='' cd -- "$bin_dir" && pwd -P)
destination="$bin_dir/devtools"
alias_destination="$bin_dir/dvt"
lock="$bin_dir/.devtools-install.lock"
installer_stage=lock
mkdir "$lock" 2>/dev/null || fail 'Another installation owns the installation lock.'
scratch=
cleanup() {
  if [ -n "$scratch" ]; then rm -rf "$scratch" 2>/dev/null; fi
  rmdir "$lock" 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP
if [ "$action" = install ]; then
  if [ -e "$destination" ] || [ -L "$destination" ]; then fail 'An installation already exists; use update.'; fi
else
  [ -f "$destination" ] && [ ! -L "$destination" ] || fail 'Update requires an existing regular executable.'
fi
installer_stage=private_staging
scratch=$(mktemp -d "$bin_dir/.devtools-install.XXXXXXXX" 2>/dev/null) || fail 'Cannot prepare installation.'

fetch() {
  case "$source" in
    https://*)
      download "${source%/}/$1" -o "$2" || fail 'Cannot download release artifact.'
      ;;
    *://*) fail 'Remote release sources must use HTTPS.' ;;
    *) cp "${source%/}/$1" "$2" 2>/dev/null || fail 'Cannot read release artifact.' ;;
  esac
}
installer_stage=artifact_fetch
fetch "$archive" "$scratch/package.tar.gz"
fetch "$archive.sha256" "$scratch/checksum"
installer_stage=artifact_checksum
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
installer_stage=archive
[ "$(tar -tzf "$scratch/package.tar.gz" 2>/dev/null)" = devtools ] || fail 'Release must contain exactly one devtools executable.'
listing=$(tar -tvzf "$scratch/package.tar.gz" 2>/dev/null) || fail 'Cannot inspect release archive.'
case "$listing" in -*) ;; *) fail 'Release executable must be a regular file.' ;; esac
tar -xzf "$scratch/package.tar.gz" -C "$scratch" devtools 2>/dev/null || fail 'Cannot extract release executable.'
[ -f "$scratch/devtools" ] && [ ! -L "$scratch/devtools" ] || fail 'Invalid release executable.'
chmod 0755 "$scratch/devtools" 2>/dev/null || fail 'Cannot set executable permissions.'
installer_stage=candidate_version
DEVTOOLS_FORMAT=json "$scratch/devtools" schema version > "$scratch/capability.json" 2> "$scratch/candidate.error" || fail 'Cannot discover the release JSON contract.'
# Candidate envelopes use the CLI's compact Go JSON encoding. This fixed metadata
# marker selects the new root option; older verified releases keep their argv.
if grep -F '"name":"format"' "$scratch/capability.json" >/dev/null && grep -F '"environment":"DEVTOOLS_FORMAT"' "$scratch/capability.json" >/dev/null && grep -F '"placement":"root-prefix"' "$scratch/capability.json" >/dev/null && grep -F '"values":["auto","text","json"]' "$scratch/capability.json" >/dev/null; then
  DEVTOOLS_FORMAT=json "$scratch/devtools" --format json version > "$scratch/version.json" 2> "$scratch/candidate.error" || fail 'Release executable cannot run on this system.'
else
  DEVTOOLS_FORMAT=json "$scratch/devtools" version > "$scratch/version.json" 2> "$scratch/candidate.error" || fail 'Release executable cannot run on this system.'
fi
grep -F '"version":"'"$version"'"' "$scratch/version.json" >/dev/null || fail 'Release version does not match the request.'
setup_intent=
if [ "$platform" = linux ]; then
  case "${XDG_DATA_HOME:-}" in /*) setup_data=$XDG_DATA_HOME ;; *) setup_data="$HOME/.local/share" ;; esac
  setup_intent="$setup_data/devtools/netsetup/intent.json"
elif [ "$platform" = darwin ]; then
  setup_intent="$HOME/Library/Application Support/devtools/data/netsetup/intent.json"
fi
restore_setup=false
if [ "$action" = update ] && [ -f "$setup_intent" ] && grep -F '"enabled":true' "$setup_intent" >/dev/null; then
  installer_stage=prepare_setup
  [ -z "${DEVTOOLS_ISOLATED_ROOT:-}" ] || fail 'Executable permission changes are unavailable in isolated mode.'
  ln "$destination" "$scratch/previous" 2>/dev/null || fail 'Cannot preserve the original executable for recovery.'
  "$scratch/devtools" __install-prepare "$destination" > "$scratch/setup.result" 2> "$scratch/setup.error" || fail 'Cannot preserve low-port setup; the original executable is unchanged. Retry interactively or use high ports.'
  restore_setup=true
fi
installer_stage=publish
installer_effects=unknown
mv -f "$scratch/devtools" "$destination" 2>/dev/null || fail 'Cannot publish installed executable.'
installer_effects=committed
installer_stage=commit_setup
if [ "$restore_setup" = true ] && ! "$destination" __install-commit "$destination" > "$scratch/setup.result" 2> "$scratch/setup.error"; then
  installer_stage=rollback
  installer_effects=unknown
  mv -f "$scratch/previous" "$destination" 2>/dev/null || fail 'Update recovery requires restoring the preserved original executable.'
  installer_effects=partial
  "$destination" __install-rollback "$destination" > "$scratch/setup.result" 2> "$scratch/setup.error" || fail 'Executable restored; setup intent requires repair with proxy setup.'
  installer_effects=none
  fail 'Cannot commit low-port setup; the original executable was restored.'
fi
alias_status=created
installer_stage=alias
if [ -L "$alias_destination" ] && [ "$(readlink "$alias_destination")" = devtools ]; then
  alias_status=unchanged
elif [ -e "$alias_destination" ] || [ -L "$alias_destination" ] || command -v dvt >/dev/null 2>&1; then
  alias_status=skipped_conflict
  printf '%s\n' 'Warning: dvt already exists on PATH or in the install directory; alias was not changed.' >&2
elif ! ln -s devtools "$alias_destination" 2>/dev/null; then
  alias_status=failed
  printf '%s\n' 'Warning: cannot create dvt alias; devtools installation completed.' >&2
fi
printf '{"schema_version":1,"ok":true,"data":{"action":"%s","version":"%s","alias":{"name":"dvt","status":"%s"}}}\n' "$action" "$version" "$alias_status"
}
