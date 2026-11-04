#!/bin/sh
set -eu

if command -v fish >/dev/null 2>&1 && command -v zsh >/dev/null 2>&1; then
  exit 0
fi
case "$(uname -s)" in
  Linux)
    sudo apt-get update
    sudo apt-get install -y --no-install-recommends fish zsh
    ;;
  Darwin)
    command -v fish >/dev/null 2>&1 || brew install fish
    command -v zsh >/dev/null 2>&1 || brew install zsh
    ;;
  *) exit 1 ;;
esac
command -v fish
command -v zsh
