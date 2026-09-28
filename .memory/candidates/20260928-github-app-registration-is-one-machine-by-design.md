---
about: code-review's GitHub App registration is deliberately machine-local, and posting or approving always needs it; reading a pull request needs it only on a machine that also holds no GH_TOKEN/GITHUB_TOKEN fallback
saw:
  - source/toolkit/internal/githubapp/credential.go
  - source/toolkit/internal/cli/codereview_initialize.go
  - source/toolkit/internal/cli/codereview.go
  - source/toolkit/internal/cli/codereview_pr.go
  - source/toolkit/internal/githubapp/client.go
  - docs/adr/0012-reviews-run-locally-not-in-ci.md
  - docs/adr/0009-the-app-can-push-so-that-it-can-approve.md
  - docs/adr/0006-the-judge-decides-agtk-transmits.md
  - docs/adr/0019-a-token-reads-a-pull-request-and-never-posts.md
---

Investigated for: making `agtk code-review` work in an ephemeral cloud container (fresh
container per session, no persistent `$XDG_CONFIG_HOME`).

**Why a GitHub App at all (not GH_TOKEN/gh CLI).** Three separate reasons, each pinned to an
ADR:
- ADR 0009: a review's approval only counts toward GitHub's required-review check when its
  author `authorCanPushToRepository`; a plain user/PR-author token can't satisfy that as itself
  without also being able to merge, so the App is granted `contents: write` and holds an
  identity distinct from the PR author (so the tool can review/approve without self-approving).
- ADR 0006: no model-invoking code holds a GitHub credential at all — only `agtk` posts, via
  exactly one API call built from the judge's JSON — so the credential lives in a package
  (`internal/githubapp`) nothing that invokes a model imports.
- ADR 0012: the whole feature is designed to run on a developer laptop with no CI runner and no
  repository secret, specifically because Codex's `auth.json` credential can't safely be shared
  across concurrent/ephemeral processes (single-use refresh tokens) — this is the part that
  breaks down for an ephemeral *agtk* container, since the constraint ADR 0012 was solving was
  about the *coding-agent* CLI credential, not the GitHub App key, but the storage design (one
  registration file under `$XDG_CONFIG_HOME`, "once per machine") was carried over uniformly.

**Storage (verified in code, `source/toolkit/internal/githubapp/credential.go`).** Two files
under `agtk`'s config dir (`userconfig.Dir()`, i.e. `$XDG_CONFIG_HOME/agentic-toolkit/` or
`~/.config/agentic-toolkit/`): `github-app.id` (plain decimal app id, mode 0600) and
`github-app.pem` (RSA private key, PKCS#1 or PKCS#8, mode 0600), directory itself checked to be
0700. `Load` refuses (doesn't just warn) if the dir or key file is group/other-accessible. No env
var of any kind is read inside `githubapp` itself or `cli/codereview_initialize.go` — the App
credential stays file-based and machine-local, unconditionally. `GH_TOKEN`/`GITHUB_TOKEN` are
read, but only in `internal/cli` (`clientSeam.token`) and only to build a read-only fallback
client when no registration exists at all; they never reach `internal/githubapp` and never
substitute for the App credential on a posting path. The one non-interactive affordance for the
App credential itself is `register --key-stdin`, which reads the PEM from stdin instead of
`--key-file`, explicitly so a key can come from a secret manager "without being written to disk
first" — this is the natural hook for a setup script
(`agtk code-review register --app-id "$APP_ID" --key-stdin <<<"$PEM"`), but it still requires the
register step to run in every fresh container that wants to post, and still lands the key on disk
at the fixed path/mode afterward (Initialize always writes both files).

**Which subcommands need it.** `resolvePullRequest` (`cli/codereview_pr.go`) builds its client
only from `clientSeam.client`, i.e. only from the registration, and both a posting `run --pr` and
`approve` resolve through it — so posting and approving always refuse on an unregistered machine,
whatever the environment holds. Reads are different: `explain --pr` and `run --pr`'s
`--dry-run`/`--no-post` paths resolve through `readPullRequest` instead, which reads as the App
when registered and otherwise falls back to a token from `GH_TOKEN` or `GITHUB_TOKEN`
(`githubapp.NewReadClient`), built in `internal/cli` rather than `internal/githubapp` so the
credential-holding package stays source-agnostic (`credential_surface_test.go`'s guard against
`os.Setenv`/`os.Environ` walks `internal/githubapp` only, not `internal/cli`). The fallback
triggers only when `githubapp.Unregistered(err)` is true — a broken/partial registration (App id
present, key missing) still refuses with "register again" rather than being read around. The
token client's type has no posting method and cannot satisfy `reviewapprove.GitHub`, so a
read-token reaching a post site is a compile error. See ADR 0019 for the full design and why
posting was excluded. Bare `explain` (no `--pr`) needs no registration at all — it is local-only
(manifest, diff profiling), per the comment block at the top of `cli/codereview.go:14-27`.

**Installation lookup.** One App, no per-repo config: `githubapp.Client.bearer()`
(`client.go:211-245`) looks up the installation id per-repo lazily via `GET
/repos/{slug}/installation` (signed with the App's JWT) the first time a client is used, caches
it in memory for that process's life, then mints a short-lived installation access token. No
separate "reviewer" vs "approver" App — one registration serves `run`, `explain --pr`, and
`approve`.

**No prior CI/headless design exists.** Found no mention of GitHub Actions, ephemeral runners,
or env-var-based registration for `code-review` outside ADR 0012, which argues *against* running
in CI at all (cost of an always-on reviewer, Codex's non-shareable credential) — that argument is
about the *model* credential, not the GitHub App key, so it doesn't by itself rule out
registering the App fresh per ephemeral container via a setup script and `--key-stdin`.
