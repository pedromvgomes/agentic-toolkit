#!/usr/bin/env bash
# Detect the most likely parent (source) branch of the current branch.
# Outputs JSON: {current, parent, base, candidates: [{branch, depth}]}
set -euo pipefail

CURRENT=$(git branch --show-current)
if [ -z "$CURRENT" ]; then
  echo '{"error": "detached HEAD or not on a branch"}'
  exit 1
fi

HEAD_SHA=$(git rev-parse HEAD)
UPSTREAM=$(git rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null || true)

# Candidate refs: every local branch except the current one, plus every remote
# branch except this branch's own remote copy (same work, would yield a base of
# the branch's own tip) and <remote>/HEAD, a symbolic alias for the default
# branch. Note that `%(refname:short)` renders refs/remotes/origin/HEAD as bare
# "origin", so the HEAD aliases must be filtered on the full refname.
CANDIDATES=$( { git for-each-ref --format='%(refname:short)' refs/heads/ | grep -vx "$CURRENT" || true
                git for-each-ref --format='%(refname)' refs/remotes/ \
                  | grep -v '/HEAD$' \
                  | sed 's|^refs/remotes/||' \
                  | while read -r r; do
                      [ -z "$r" ] && continue
                      [ "${r#*/}" = "$CURRENT" ] && continue   # origin/<this branch>
                      [ -n "$UPSTREAM" ] && [ "$r" = "$UPSTREAM" ] && continue
                      printf '%s\n' "$r"
                    done
              } \
  | while read -r b; do
      [ -z "$b" ] && continue
      mb=$(git merge-base HEAD "$b" 2>/dev/null) || continue
      [ "$mb" = "$HEAD_SHA" ] && continue   # b is a descendant of HEAD
      depth=$(git rev-list --count "$mb..HEAD")
      printf '%s %s\n' "$depth" "$b"
    done \
  | sort -n)

PARENT=$(echo "$CANDIDATES" | head -1 | awk '{print $2}')

# Fall back to the repo's default branch when nothing shares history with HEAD
# (e.g. a single-branch repo, or a root-commit-only history).
if [ -z "$PARENT" ]; then
  DEFAULT=$(git symbolic-ref --short refs/remotes/origin/HEAD 2>/dev/null || true)
  if [ -z "$DEFAULT" ]; then
    for b in main master trunk develop; do
      if git rev-parse --verify --quiet "refs/heads/$b" >/dev/null; then DEFAULT="$b"; break; fi
      if git rev-parse --verify --quiet "refs/remotes/origin/$b" >/dev/null; then DEFAULT="origin/$b"; break; fi
    done
  fi
  PARENT="$DEFAULT"
fi

if [ -z "$PARENT" ] || ! BASE=$(git merge-base HEAD "$PARENT" 2>/dev/null); then
  printf '{"current":"%s","error":"could not resolve a parent branch sharing history with HEAD; pass a base ref explicitly"}\n' "$CURRENT"
  exit 1
fi

CAND_JSON=$(echo "$CANDIDATES" \
  | awk 'NF {printf "%s{\"branch\":\"%s\",\"depth\":%s}", (NR>1?",":""), $2, $1}')

cat <<EOF
{"current":"$CURRENT","parent":"$PARENT","base":"$BASE","candidates":[$CAND_JSON]}
EOF
