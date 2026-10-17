#!/bin/sh
set -eu
exec python3 "$(dirname "$0")/project.py" package-skill "${VERSION:?Set VERSION to the release version}"
