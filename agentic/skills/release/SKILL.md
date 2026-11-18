---
name: release
description: >-
  Use this skill when the user asks to release, publish, cut or tag a version of agtk in this repo
  (e.g. "release v0.22.0", "cut a patch release", "ship a release"). Covers choosing the version,
  writing the release notes, merging them, and verifying the release the workflow makes. Nobody
  pushes a tag.
---

# Release agtk

A release is the merge of its notes (ADR 0030). The pull request that adds `docs/releases/vX.Y.Z.md`
merges to `main`, and `auto-release.yml` then tags that commit `vX.Y.Z` as `github-actions[bot]` and
publishes the GitHub Release with goreleaser. **No one pushes a tag**, and a tag pushed by hand
releases nothing.

The tag is permanent: a wrong release is superseded by a new version, never moved or deleted.

## 1 — Decide the version

The tags and the full history have to be local first. A shallow clone holds no tags and a truncated
history, so the lookup below prints nothing and the bump is chosen from the wrong commits without
anything failing.

```bash
git fetch --tags origin
if [ "$(git rev-parse --is-shallow-repository)" = true ]; then git fetch --unshallow origin; fi
last="$(git tag --list 'v*' --sort=-v:refname | head -1)"
git log --format='%h %s' "$last..origin/main"
```

Pick the bump from the Conventional Commit types since the last tag: a `!` or `BREAKING CHANGE` is
major, `feat` is minor, anything else is patch. Before `v1.0.0` a breaking change is a minor bump.
When the user names the version, use it; if the commits say it is a larger bump than the user asked
for, say so in the notes rather than silently overriding it.

A major version of `v2` or above changes the module path. Stop and confirm with the user before
releasing one.

## 2 — Check nothing is pending

A pending version is one whose notes exist and whose tag does not. There must be none besides this
one, or the plan fails:

```bash
git tag --list 'v*' | sort > /tmp/tags
gh api "repos/$(gh repo view --json nameWithOwner --jq .nameWithOwner)/releases" --paginate --jq '.[].tag_name' | sort > /tmp/releases
.github/scripts/release/pending.sh docs/releases /tmp/tags /tmp/releases
```

`release` names a version already waiting: it is released by the next push to `main`, so do not
write notes for a second one on top of it. `finish` names a tag with notes and no GitHub Release,
which the next sweep completes.

## 3 — Write the release notes

`docs/releases/vX.Y.Z.md`, on a branch off `main`. **The file is the release**: merging it releases
the version, and the workflow fails without it before it tags anything. It becomes the body of the
GitHub Release, so write it for a consumer: what changed, and what they have to do about it. A
breaking change names the old and new spelling; "various fixes" is not release notes.

Start from the shape of the previous notes (`docs/releases/v0.21.0.md`): a `## vX.Y.Z — summary`
heading, then a section per change, with the breaking ones first and named as such.

## 4 — Open the pull request

The title is `chore(release): vX.Y.Z`, conventional and lower-case after the colon. The pull request
carries the notes file and nothing else that is not part of the release. `open-pr` opens it.

## 5 — Merge it, then watch

The merge is the release. After it:

```bash
gh run list --workflow auto-release --limit 3
gh run watch <run-id>
```

`auto-release.yml` plans the pending version and calls `release.yml`, whose jobs are `verify`
(read-only; the notes exist, the tag does not, the commit is on `main`, `make check` passes), `tag`
(write; one annotated tag, no project code) and `release` (write; goreleaser at the tag).

A failure rolls nothing back. A later push to `main` sweeps again and finishes what is pending or
untagged. To finish a tag that exists without a GitHub Release at once:

```bash
gh workflow run release.yml -f tag=vX.Y.Z
```

To check the plan without releasing, dispatch `auto-release` on `main` with `dry_run` set.

## 6 — Verify

```bash
gh release view vX.Y.Z
gh release view vX.Y.Z --json assets --jq '.assets[].name'   # four archives and checksums.txt
agtk update --check
```

The archives are `agtk_<version>_<os>_<arch>.tar.gz` for darwin and linux on amd64 and arm64, and
`checksums.txt` verifies them. `agtk update` reads these releases, so it should offer the new
version.
