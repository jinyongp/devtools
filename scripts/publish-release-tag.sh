#!/bin/sh
exec python3 "$(dirname "$0")/publish_release_tag.py" "$@"
