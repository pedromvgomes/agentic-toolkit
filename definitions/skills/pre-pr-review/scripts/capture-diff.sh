#!/usr/bin/env bash
# Emit the full set of changes to review: committed-vs-base, all worktree
# changes (staged and unstaged together), and untracked files.
#
# usage: capture-diff.sh <base-ref> [pathspec...]
#
# Each pathspec is a path or glob relative to the repo root. A pathspec
# prefixed with '!' excludes matching files; any other pathspec restricts the
# output to matching files. Excludes always win.
#
# A pathspec containing no '/' is treated as a basename and matched at any
# depth, so '!package-lock.json' drops every lockfile in a monorepo rather than
# only the one at the root. Anchor to the root by writing a path with a slash.
#
#   capture-diff.sh main '!package-lock.json' '!vendor/**'   # everything but those
#   capture-diff.sh main '**/*.kt' '**/*.kts'                # only Kotlin sources
set -euo pipefail

# Run from the repo root so every emitted path — including the untracked ones
# from git ls-files, which are printed relative to the cwd — is repo-relative.
cd "$(git rev-parse --show-toplevel)"

BASE="${1:?usage: capture-diff.sh <base-ref> [pathspec...]}"
shift

INCLUDES=()
EXCLUDES=()
for spec in "$@"; do
  case "$spec" in
    !*) raw="${spec#!}"; kind=exclude ;;
    *)  raw="$spec";     kind=include ;;
  esac
  case "$raw" in
    */*) ;;                 # already a path — anchor it at the root
    *)   raw="**/$raw" ;;   # bare basename — match at any depth
  esac
  if [ "$kind" = exclude ]; then
    EXCLUDES+=(":(top,exclude,glob)$raw")
  else
    INCLUDES+=(":(top,glob)$raw")
  fi
done
[ ${#INCLUDES[@]} -eq 0 ] && INCLUDES=(":(top)")
PATHSPEC=("${INCLUDES[@]}" ${EXCLUDES[@]+"${EXCLUDES[@]}"})

echo "=== diff vs ${BASE} (committed) ==="
git diff "${BASE}...HEAD" -- "${PATHSPEC[@]}"
echo ""
echo "=== diff worktree (staged + unstaged, vs HEAD) ==="
git diff HEAD -- "${PATHSPEC[@]}"
echo ""
echo "=== untracked files ==="
while IFS= read -r f; do
  [ -z "$f" ] && continue
  # --no-index exits 1 whenever it finds a difference, which is always here.
  git diff --no-index -- /dev/null "$f" || true
done < <(git ls-files --others --exclude-standard --full-name -- "${PATHSPEC[@]}")
