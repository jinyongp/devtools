#!/usr/bin/env python3
"""Publish the already validated exact release commit; also used by fixture tests."""
import os
import re
import subprocess
import sys


def git(*args):
    return subprocess.check_output(["git", *args], text=True, encoding="utf-8").strip()


def publish():
    tag, commit = os.environ.get("TAG", ""), os.environ.get("COMMIT", "")
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?", tag) or not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("Invalid TAG or COMMIT")
    if git("rev-parse", "HEAD") != commit:
        raise ValueError("HEAD does not match COMMIT")
    git("fetch", "--no-tags", "origin", "main")
    git("merge-base", "--is-ancestor", commit, "FETCH_HEAD")
    remote = git("ls-remote", "origin", "refs/tags/" + tag, "refs/tags/" + tag + "^{}")
    if remote:
        rows = [line.split() for line in remote.splitlines()]
        target = next((row[0] for row in rows if row[1].endswith("^{}")), rows[0][0])
        if target != commit:
            raise ValueError(tag + " already belongs to another commit; refusing to replace it.")
        print(f"Reusing {tag} at {commit}.")
        return
    exists = subprocess.run(["git", "show-ref", "--verify", "--quiet", "refs/tags/" + tag]).returncode
    if exists == 0:
        if git("rev-parse", tag + "^{commit}") != commit:
            raise ValueError(tag + " exists locally at another commit; refusing to replace it.")
    elif exists == 1:
        git("-c", "user.name=github-actions[bot]", "-c", "user.email=41898282+github-actions[bot]@users.noreply.github.com", "tag", "-a", tag, commit, "-m", "Release " + tag)
    else:
        raise ValueError("Cannot inspect existing release tag")
    git("push", "origin", "refs/tags/" + tag + ":refs/tags/" + tag)


if __name__ == "__main__":
    try:
        if len(sys.argv) != 1:
            raise ValueError("Usage: publish_release_tag.py (TAG and COMMIT are required)")
        publish()
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print(error, file=sys.stderr)
        sys.exit(1)
