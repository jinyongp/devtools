#!/bin/sh
# Compatibility entry for existing Unix callers.
exec python3 "$(dirname "$0")/project.py" "$@"
