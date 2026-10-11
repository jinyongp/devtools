#!/bin/sh
set -eu

version=0.0.0-installer-test
scratch=$(mktemp -d "${TMPDIR:-/tmp}/devtools-installer-check.XXXXXXXX")
trap 'rm -rf "$scratch"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

case "$(uname -s)" in
  Linux) platform=linux ;;
  Darwin) platform=darwin ;;
  *) echo 'Unsupported test operating system.' >&2; exit 2 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo 'Unsupported test architecture.' >&2; exit 2 ;;
esac

release="$scratch/release"
bin_dir="$scratch/bin"
home="$scratch/home"
XDG_DATA_HOME="$scratch/data"
export XDG_DATA_HOME
mkdir -p "$release" "$home"

cat > "$scratch/devtools" <<'EOF'
#!/bin/sh
if [ "${1:-}" = version ]; then
  printf '%s\n' '{"schema_version":1,"ok":true,"data":{"version":"0.0.0-installer-test"}}'
  exit 0
fi
case "${1:-}" in
  __install-prepare) [ "${DEVTOOLS_INSTALL_TEST_FAIL_PREPARE:-}" != true ]; exit $? ;;
  __install-commit) [ "${DEVTOOLS_INSTALL_TEST_FAIL_COMMIT:-}" != true ]; exit $? ;;
  __install-rollback) exit 0 ;;
esac
exit 2
EOF
chmod 0755 "$scratch/devtools"

archive="devtools_${version}_${platform}_${arch}.tar.gz"
tar -czf "$release/$archive" -C "$scratch" devtools
(
  cd "$release"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$archive" > "$archive.sha256"
  else
    shasum -a 256 "$archive" > "$archive.sha256"
  fi
)

HOME="$home" sh scripts/install.sh \
  --version "$version" \
  --source "$release" \
  --bin-dir "$bin_dir" > "$scratch/result.json"

grep -F '"action":"install"' "$scratch/result.json" >/dev/null
[ -x "$bin_dir/devtools" ]
"$bin_dir/devtools" version | grep -F '"version":"'"$version"'"' >/dev/null

# Relative destinations are relative to the caller, even with a shell CDPATH.
installer="$(CDPATH='' cd -- scripts && pwd -P)/install.sh"
mkdir -p "$scratch/caller/bin" "$scratch/elsewhere/bin"
(
  cd "$scratch/caller"
  HOME="$home" CDPATH="$scratch/elsewhere" sh "$installer" \
    --version "$version" --source "$release" --bin-dir bin > "$scratch/relative.json"
)
[ -x "$scratch/caller/bin/devtools" ]
[ ! -e "$scratch/elsewhere/bin/devtools" ]

# Existing setup intent makes update preparation explicit. A preparation or
# post-publish commit failure preserves the exact original executable.
mkdir -p "$XDG_DATA_HOME/devtools/netsetup"
printf '%s\n' '{"enabled":true}' > "$XDG_DATA_HOME/devtools/netsetup/intent.json"
cp "$bin_dir/devtools" "$scratch/original"
printf '%s\n' '# release candidate marker' >> "$scratch/devtools"
tar -czf "$release/$archive" -C "$scratch" devtools
(
  cd "$release"
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$archive" > "$archive.sha256"; else shasum -a 256 "$archive" > "$archive.sha256"; fi
)
if HOME="$home" DEVTOOLS_INSTALL_TEST_FAIL_PREPARE=true sh "$installer" update --version "$version" --source "$release" --bin-dir "$bin_dir" > "$scratch/failed-prepare.json" 2>&1; then
  echo 'Update unexpectedly bypassed failed setup preparation.' >&2; exit 1
fi
cmp "$scratch/original" "$bin_dir/devtools"
if HOME="$home" DEVTOOLS_INSTALL_TEST_FAIL_COMMIT=true sh "$installer" update --version "$version" --source "$release" --bin-dir "$bin_dir" > "$scratch/failed-commit.json" 2>&1; then
  echo 'Update unexpectedly bypassed failed setup commit.' >&2; exit 1
fi
cmp "$scratch/original" "$bin_dir/devtools"
HOME="$home" sh "$installer" update --version "$version" --source "$release" --bin-dir "$bin_dir" > "$scratch/updated.json"
grep -F '# release candidate marker' "$bin_dir/devtools" >/dev/null
