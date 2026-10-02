#!/bin/sh
set -eu

: "${TAG:?Set TAG to the release tag}"
: "${COMMIT:?Set COMMIT to the full release commit}"
printf '%s\n' "$TAG" | LC_ALL=C grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$'
printf '%s\n' "$COMMIT" | LC_ALL=C grep -Eq '^[0-9a-f]{40}$'
[ "$(git rev-parse HEAD)" = "$COMMIT" ]
git fetch --no-tags origin main
git merge-base --is-ancestor "$COMMIT" FETCH_HEAD

remote=$(git ls-remote origin "refs/tags/$TAG" "refs/tags/$TAG^{}")
if [ -n "$remote" ]; then
  target=$(printf '%s\n' "$remote" | awk 'NR == 1 { target = $1 } /\^\{\}$/ { target = $1 } END { print target }')
  [ "$target" = "$COMMIT" ] || {
    echo "$TAG already belongs to another commit; refusing to replace it." >&2
    exit 1
  }
  printf 'Reusing %s at %s.\n' "$TAG" "$COMMIT"
  exit 0
fi

if git show-ref --verify --quiet "refs/tags/$TAG"; then
  [ "$(git rev-parse "$TAG^{commit}")" = "$COMMIT" ] || {
    echo "$TAG exists locally at another commit; refusing to replace it." >&2
    exit 1
  }
else
  git -c user.name='github-actions[bot]' \
    -c user.email='41898282+github-actions[bot]@users.noreply.github.com' \
    tag -a "$TAG" "$COMMIT" -m "Release $TAG"
fi
git push origin "refs/tags/$TAG:refs/tags/$TAG"
