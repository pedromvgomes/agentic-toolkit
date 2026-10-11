---
about: "review.RemoteSlug is the only git-remote parser; it reads remote.<n>.url via git config (not get-url), refuses any path that is not exactly owner/repo, and so rejects the cloud local-proxy remote; no ADR fixes a Project-derivation rule"
saw:
  - source/toolkit/internal/review/pr.go
  - source/toolkit/internal/review/tests/pr_test.go
  - source/toolkit/internal/review/git.go
  - CONTEXT.md
  - docs/adr/0025-usage-data-lives-in-d1-behind-a-worker-and-cost-is-computed-at-query-time.md
---

Found while planning issue #147 (resolve Project/Repo for a usage row).

- `review.RemoteSlug(dir, remote)` (`review/pr.go:44`) is the only remote parser in `internal/`. `DefaultRemote = "origin"` (`pr.go:12`); every caller passes it (`cli/codereview_post.go:76`, `codereview_pr.go:233,267,340`). No "first remote" or "upstream" logic exists.
- It runs `git config --get remote.<n>.url` (`pr.go:54`), deliberately not `git remote get-url`, which applies `url.<base>.insteadOf` (comment `pr.go:48-53`).
- `slugRE` (`pr.go:32`) is end-anchored: scp-style, https, `ssh://`, optional `user@`, optional port, optional `.git`, optional trailing `/`; any host (no github.com check). A path with more than two segments is refused rather than truncated. Tests: `review/tests/pr_test.go:13-26` (scp, https, `ssh://`, `.git.git`) and the nested-host refusal below it. By reading the regex, `http://local_proxy@127.0.0.1:PORT/git/owner/repo` has three path segments and is refused (not run against a real cloud session). A Project derivation that must work in cloud sessions needs its own handling of that form.
- Its package-private `git` helper (`review/git.go:21,35`) shells out with `exec.Command("git")`, `cmd.Dir = dir`, and `GIT_LITERAL_PATHSPECS=1`; there is no timeout and no `git`-absent handling.
- `internal/review` is in the model-invoking set barred from reaching `internal/githubapp` (`cli/tests/credential_surface_test.go`, `TestTheModelInvokingPackagesCannotReachTheCredential`), so importing it is allowed.
- No ADR or CONTEXT.md entry fixes the derivation rule. CONTEXT.md "Project" (~line 681) says only "taken from an environment variable when one is set, otherwise derived from the repository the session ran in". Neither the env var name (AGTK_PROJECT) nor the remote-then-basename fallback is written down in the repo (grep of ADRs 0025/0026 and CONTEXT.md).
