# A release is the merge of its notes

Status: accepted.

A version is released when a pull request that adds `docs/releases/vX.Y.Z.md` merges to `main`.
The workflow `auto-release.yml` then tags that commit `vX.Y.Z` and publishes the GitHub Release with
goreleaser. The tag form and the archives are unchanged; what changes is who creates the tag: the
workflow, from the merge commit, and no person.

## The rules

- **Merging the notes is the release.** The only human act is merging a pull request that adds the
  notes of one version. `auto-release.yml` runs on every push to `main` and releases the version
  whose notes exist and whose tag does not.
- **The notes are the release notes.** goreleaser is given the merged file as the body of the
  GitHub Release, and no generated text replaces it. A version without notes cannot be released.
- **Tags are unsigned annotated tags** made with `GITHUB_TOKEN` as `github-actions[bot]`. The tag
  message carries the URL of the run that made it.
- **`release.yml` has no tag-push trigger.** A tag pushed by hand runs nothing. A tag pushed with
  `GITHUB_TOKEN` starts no workflow either, so the release is a reusable workflow that
  `auto-release.yml` calls, not one a tag triggers. A `workflow_dispatch` with the tag finishes a
  partial release.
- **Every run sweeps.** A run releases the pending version and finishes every tag that has notes and
  no GitHub Release. A failure rolls nothing back: the next push retries from its own commit, and a
  re-run retries at the original commit. More than one pending version fails the plan, because
  releasing both would tag the older notes at the newer commit.
- **The permission seam.** In `release.yml`, `verify` holds `contents: read`, checks out with
  `persist-credentials: false`, and runs the repository's checks. `tag` holds `contents: write`
  and runs no project code. `release` holds `contents: write` and builds only at the tag `verify`
  validated.
- **Concurrency.** `auto-release` uses one group without cancelling, and a run cancelled while queued
  is harmless because the next run sweeps. Inside `release.yml` the group is keyed per version, so a
  dispatch that finishes a release and a sweep never publish one tag at once.
- **A tag that exists at release time** was made by a concurrent run. A rejected tag push fails that
  run, and the next sweep no longer sees the version as pending.
- **A tag made by hand.** It triggers nothing. A sweep finishes a tag that has notes and no GitHub
  Release only when its commit is on `main` and carries the notes, so a tag on a commit nobody
  reviewed releases nothing, and a tag on a reviewed commit publishes what the merge already
  approved. The tagger identity is not checked as provenance: anyone who can push a tag can set it.
- **Edge cases.** A notes file whose name is not a version is ignored, and a tag without notes is
  never finished.

## Considered options

- **A tag pushed by hand.** A trigger on tag push is a second path to a release that skips
  the sweep, and a hand-pushed tag runs against whatever commit its author chose. One path, the merge
  of the notes, is the one a reviewer approved. A partial release is finished by dispatching
  `release.yml`, which validates at the tag's commit.
- **Tags signed in CI.** A signing key in the workflow is a publication credential the repository
  does not otherwise hold, and the signature would say only that the CI ran, which the annotated
  tag's message and the bot identity already say. A tag ruleset on `v*`, which lets only the workflow
  create one, is repository hardening a person applies.
- **A single job that tests, tags and publishes.** A job that runs the repository's checks and then
  pushes a tag has the write token in reach of that code. The `verify`, `tag` and `release` seam
  keeps the token out of the job that runs the checks.
- **Release notes generated from commit history.** A changelog generated at release time is text no
  reviewer read before the tag existed. The notes are the merged file, and the review of the notes
  is the review of the release.

## Consequences

- **Merging the notes cannot be undone by the workflow.** A tag is permanent, so a notes file that
  merges releases the version. The review of the notes is the review of the release.
- **A release is a pull request.** Cutting one needs the version chosen, the notes written and the
  pull request merged; the `release` skill carries the procedure.
- **Every push to `main` runs `plan`.** A push that adds no notes finds nothing pending and releases
  nothing.
- **A failed publication is retried by the next sweep.** The tag exists, its commit carries the
  notes and there is no GitHub Release, so the next run finishes it.
- **The release logic has an offline test.** `.github/scripts/release/test/pending_test.sh` runs in
  the `verify` job and covers what a sweep plans.
