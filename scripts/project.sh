#!/bin/sh
# Multi-step project commands; simple commands live directly in devtools.toml.
set -eu

action=${1:?Project action required}
shift
case "$action" in
  dashboard)
    go build -o bin/dashboard ./scripts/dashboard
    exec ./bin/dashboard "$@"
    ;;
  check)
    # Prefer this checkout's CLI for nested commands, including clean CI hosts.
    runner=devtools
    if [ -x ./bin/devtools ]; then runner=./bin/devtools; fi
    "$runner" run check:skill-release
    "$runner" run check:dashboard
    "$runner" run check:installer
    "$runner" run check:release
    "$runner" run check:project
    "$runner" run check:perf
    test -z "$(gofmt -l cmd internal scripts skills perf)"
    go vet ./...
    exec go test -race ./... "$@"
    ;;
  release-check)
    sh -n scripts/project.sh
    sh -n scripts/publish-release-tag.sh
    node --test scripts/release.test.mjs
    exec python3 -B -m unittest discover -s verify -p test_run.py "$@"
    ;;
  installer-check)
    sh -n scripts/install.sh
    sh scripts/check-installer.sh
    DEVTOOLS_TEST_README=README.md DEVTOOLS_TEST_INSTALL_DOC=docs/install.md \
      exec python3 -B verify/scenarios/bootstrap.py "$@"
    ;;
  package-cli|package-skill)
    if [ "$#" -ne 1 ]; then
      echo "Usage: devtools run release:${action#package-} VERSION" >&2
      exit 2
    fi
    VERSION=$1
    export VERSION
    if [ "$action" = package-cli ]; then exec sh scripts/package.sh; fi
    exec sh scripts/package-skill.sh
    ;;
  *) echo "Unknown project action: $action" >&2; exit 2 ;;
esac
