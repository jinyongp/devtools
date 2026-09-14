#!/bin/sh
set -eu

if ! command -v uvx >/dev/null 2>&1; then
  echo 'uvx is required to validate the Agent Skill release.' >&2
  exit 127
fi

scratch=$(mktemp -d "${TMPDIR:-/tmp}/devtools-skill-check.XXXXXXXX")
trap 'rm -rf "$scratch"' EXIT

sh scripts/validate-skill.sh
OUTPUT_DIR="$scratch/release-a" VERSION=0.0.0-test sh scripts/package-skill.sh >/dev/null
TZ=Asia/Seoul OUTPUT_DIR="$scratch/release-b" VERSION=0.0.0-test sh scripts/package-skill.sh >/dev/null
archive="$scratch/release-a/devtools-skill_0.0.0-test.tar.gz"
checksum="$archive.sha256"
sh scripts/verify-skill-package.sh "$archive" "$checksum"
cmp "$scratch/release-a/devtools-skill_0.0.0-test.tar.gz" \
  "$scratch/release-b/devtools-skill_0.0.0-test.tar.gz" >/dev/null || {
  echo 'Repeated skill packages are not reproducible.' >&2
  exit 1
}
cmp "$scratch/release-a/devtools-skill_0.0.0-test.tar.gz.sha256" \
  "$scratch/release-b/devtools-skill_0.0.0-test.tar.gz.sha256" >/dev/null || {
  echo 'Repeated skill checksums are not reproducible.' >&2
  exit 1
}

mkdir -p "$scratch/failing-bin" "$scratch/failed-release"
printf '#!/bin/sh\nexit 42\n' > "$scratch/failing-bin/go"
chmod 755 "$scratch/failing-bin/go"
if PATH="$scratch/failing-bin:$PATH" OUTPUT_DIR="$scratch/failed-release" \
  VERSION=0.0.0-test sh scripts/package-skill.sh >/dev/null 2>&1; then
  echo 'Skill packaging ignored an archive writer failure.' >&2
  exit 1
fi
[ ! -e "$scratch/failed-release/devtools-skill_0.0.0-test.tar.gz" ]
[ ! -e "$scratch/failed-release/devtools-skill_0.0.0-test.tar.gz.sha256" ]

mkdir -p "$scratch/invalid"
cp -R skills/devtools "$scratch/invalid/devtools"
sed '/^description:/d' skills/devtools/SKILL.md > "$scratch/invalid/devtools/SKILL.md"
if uvx --from skills-ref==0.1.1 agentskills validate "$scratch/invalid/devtools" >/dev/null 2>&1; then
  echo 'Official validation accepted a skill without a description.' >&2
  exit 1
fi

mkdir -p "$scratch/tampered/devtools" "$scratch/tampered-release"
cp skills/devtools/SKILL.md "$scratch/tampered/devtools/SKILL.md"
printf '\n<!-- tampered -->\n' >> "$scratch/tampered/devtools/SKILL.md"
tar -czf "$scratch/tampered-release/devtools-skill_0.0.0-test.tar.gz" -C "$scratch/tampered" devtools
(
  cd "$scratch/tampered-release"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum devtools-skill_0.0.0-test.tar.gz > devtools-skill_0.0.0-test.tar.gz.sha256
  else
    shasum -a 256 devtools-skill_0.0.0-test.tar.gz > devtools-skill_0.0.0-test.tar.gz.sha256
  fi
)
if sh scripts/verify-skill-package.sh \
  "$scratch/tampered-release/devtools-skill_0.0.0-test.tar.gz" \
  "$scratch/tampered-release/devtools-skill_0.0.0-test.tar.gz.sha256" >/dev/null 2>&1; then
  echo 'Package verification accepted content that differs from the canonical skill.' >&2
  exit 1
fi

mkdir -p "$scratch/symlink/devtools" "$scratch/symlink-release"
ln -s skills/devtools/SKILL.md "$scratch/symlink/devtools/SKILL.md"
tar -czf "$scratch/symlink-release/devtools-skill_0.0.0-test.tar.gz" -C "$scratch/symlink" devtools
(
  cd "$scratch/symlink-release"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum devtools-skill_0.0.0-test.tar.gz > devtools-skill_0.0.0-test.tar.gz.sha256
  else
    shasum -a 256 devtools-skill_0.0.0-test.tar.gz > devtools-skill_0.0.0-test.tar.gz.sha256
  fi
)
if sh scripts/verify-skill-package.sh \
  "$scratch/symlink-release/devtools-skill_0.0.0-test.tar.gz" \
  "$scratch/symlink-release/devtools-skill_0.0.0-test.tar.gz.sha256" >/dev/null 2>&1; then
  echo 'Package verification accepted a symlink member.' >&2
  exit 1
fi
