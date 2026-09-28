---
description: A GitHub client built from a bare token (not the App registration) must be structurally unable to post.
---

# A token-backed GitHub client must never gain a method that writes to GitHub

Posting under any identity but the App's is invisible to the `ByViewer`-keyed bookkeeping that
decides which head was reviewed, which thread is a finding's, and what approval may count (see
ADR 0019). Make that impossible by type — no write method, no exported handle to the wrapped
client — so a token reaching a post is a compile error rather than a runtime check someone has
to remember to add.

## Applies to

`internal/githubapp.ReadClient`, and any future client built from `GH_TOKEN`/`GITHUB_TOKEN` or
another ambient credential. `internal/cli`'s `pullRequestReader` interface, which such a client
must keep satisfying without also satisfying anything that posts.

## Example

Good: `ReadClient` wraps an unexported `*Client` and exposes only `ReadPullRequest`,
`ReadReviewThreads`, `ReadSubmittedReviews`.

Bad: exporting the wrapped client, or adding `CreateReview`/`CreateFileComment` to `ReadClient`
— `TestATokenClientReadsAndNothingElse` in
`source/toolkit/internal/cli/tests/credential_surface_test.go` fails if either happens.
