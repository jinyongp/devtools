#!/bin/sh
set -eu

if ! command -v uvx >/dev/null 2>&1; then
  echo 'uvx is required to validate the Agent Skill.' >&2
  exit 127
fi

exec uvx --from skills-ref==0.1.1 agentskills validate skills/devtools
