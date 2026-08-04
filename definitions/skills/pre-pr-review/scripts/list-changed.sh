#!/usr/bin/env bash
# Emit the name-status lists matching capture-diff.sh, section for section.
#
# usage: list-changed.sh <base-ref> [pathspec...]
#
# Pathspecs follow the same rules as capture-diff.sh: a '!' prefix excludes,
# anything else restricts the output to matching files, and a spec with no '/'
# is matched at any depth.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

BASE="${1:?usage: list-changed.sh <base-ref> [pathspec...]}"
shift

INCLUDES=()
EXCLUDES=()
for spec in "$@"; do
  case "$spec" in
    !*) raw="${spec#!}"; kind=exclude ;;
    *)  raw="$spec";     kind=include ;;
  esac
  case "$raw" in
    */*) ;;
    *)   raw="**/$raw" ;;
  esac
  if [ "$kind" = exclude ]; then
    EXCLUDES+=(":(top,exclude,glob)$raw")
  else
    INCLUDES+=(":(top,glob)$raw")
  fi
done
[ ${#INCLUDES[@]} -eq 0 ] && INCLUDES=(":(top)")
PATHSPEC=("${INCLUDES[@]}" ${EXCLUDES[@]+"${EXCLUDES[@]}"})

echo "=== name-status vs ${BASE} (committed) ==="
git diff --name-status "${BASE}...HEAD" -- "${PATHSPEC[@]}"
echo ""
echo "=== name-status worktree (staged + unstaged, vs HEAD) ==="
git diff --name-status HEAD -- "${PATHSPEC[@]}"
echo ""
echo "=== untracked files ==="
git ls-files --others --exclude-standard --full-name -- "${PATHSPEC[@]}" | awk 'NF {print "A\t" $0}'
