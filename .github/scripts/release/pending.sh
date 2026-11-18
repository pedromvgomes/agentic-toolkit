#!/usr/bin/env bash
# pending.sh — names what a sweep of main has to release or finish.
#
# usage: pending.sh <notes-dir> <tags-file> <releases-file>
#
#   notes-dir      the directory holding vX.Y.Z.md release notes, read from the
#                  working tree at the commit being swept
#   tags-file      one existing tag per line
#   releases-file  the tag of each existing GitHub Release, one per line
#
# prints one JSON object:
#
#   {"release": "vX.Y.Z" | null, "finish": ["vX.Y.Z", ...]}
#
# `release` is the version whose notes exist and whose tag does not. `finish` is
# every tag that has notes and no GitHub Release: a release whose announcement
# failed. A notes file whose name is not a version is ignored, and a tag without
# notes is never finished, so a tag made by hand releases nothing. More than one
# pending version fails: two merges landed before a sweep ran, and releasing
# both would tag the older notes at the newer commit.
set -euo pipefail

notes_dir="${1:?notes directory}"
tags_file="${2:?tags file}"
releases_file="${3:?releases file}"

version_re='^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'

mapfile -t noted < <(
  for f in "$notes_dir"/v*.md; do
    [[ -e "$f" ]] || continue
    v="$(basename "$f" .md)"
    [[ "$v" =~ $version_re ]] && echo "$v"
  done | sort -V
)

has_line() { grep -qxF -- "$1" "$2"; }

pending=()
finish=()
for v in "${noted[@]}"; do
  if ! has_line "$v" "$tags_file"; then
    pending+=("$v")
  elif ! has_line "$v" "$releases_file"; then
    finish+=("$v")
  fi
done

if ((${#pending[@]} > 1)); then
  echo "::error::more than one version is pending: ${pending[*]}" >&2
  exit 1
fi

release="${pending[0]:-}"
jq -cn --arg release "$release" --args '{release: (if $release == "" then null else $release end), finish: $ARGS.positional}' "${finish[@]}"
