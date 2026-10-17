#!/bin/sh
exec python3 "$(dirname "$0")/project.py" verify-skill "$@"
