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
mkdir -p "$release" "$home"

cat > "$scratch/devtools" <<'EOF'
#!/bin/sh
if [ "${1:-}" = version ]; then
  printf '%s\n' '{"schema_version":1,"ok":true,"data":{"version":"0.0.0-installer-test"}}'
  exit 0
fi
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
