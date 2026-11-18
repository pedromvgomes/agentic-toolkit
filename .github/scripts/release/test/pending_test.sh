#!/usr/bin/env bash
# Offline test of pending.sh: each case builds a notes directory, a tags file and a
# releases file, and compares what a sweep names with what it should.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
pending="$here/../pending.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
failures=0

# case <name> <notes> <tags> <releases> <expected json>   (lists are space-separated)
check() {
  local name="$1" notes="$2" tags="$3" releases="$4" want="$5" got
  rm -rf "$work/notes"; mkdir -p "$work/notes"
  for n in $notes; do : > "$work/notes/$n"; done
  printf '%s\n' $tags > "$work/tags"
  printf '%s\n' $releases > "$work/releases"
  got="$("$pending" "$work/notes" "$work/tags" "$work/releases")"
  if [[ "$got" != "$want" ]]; then
    echo "FAIL $name: got $got, want $want"
    failures=$((failures + 1))
  fi
}

check "nothing pending" "v0.1.0.md" "v0.1.0" "v0.1.0" '{"release":null,"finish":[]}'
check "notes without a tag are released" "v0.1.0.md v0.2.0.md" "v0.1.0" "v0.1.0" '{"release":"v0.2.0","finish":[]}'
check "a tag with notes and no release is finished" "v0.1.0.md" "v0.1.0" "" '{"release":null,"finish":["v0.1.0"]}'
check "a tag without notes is never finished" "" "v0.1.0" "" '{"release":null,"finish":[]}'
check "a notes file that is not a version is ignored" "README.md v1.md notes.md" "" "" '{"release":null,"finish":[]}'
check "a pre-release version is released" "v0.2.0-rc.1.md" "" "" '{"release":"v0.2.0-rc.1","finish":[]}'
check "an empty directory releases nothing" "" "" "" '{"release":null,"finish":[]}'

rm -rf "$work/notes"; mkdir -p "$work/notes"
: > "$work/notes/v0.1.0.md"; : > "$work/notes/v0.2.0.md"; : > "$work/tags"; : > "$work/releases"
if "$pending" "$work/notes" "$work/tags" "$work/releases" >/dev/null 2>&1; then
  echo "FAIL two pending versions: pending.sh succeeded"
  failures=$((failures + 1))
fi

((failures == 0)) && echo "pending_test: ok"
exit "$failures"
