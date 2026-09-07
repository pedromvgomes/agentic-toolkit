# Bot reviews run locally and post as a GitHub App; there is no CI runner

`agtk code-review` runs on a developer's own machine, against the CLIs that machine is already
logged into, and posts the result to the PR through a GitHub App. There is no workflow file, no
runner, and no model credential in any repository secret. The App's private key lives on the
machine (`~/.config/agtk/`), so `initialize` is a once-per-machine registration and adding a
repo is one App installation rather than a per-repo secret ceremony.

The shape follows from what the two coding-agent CLIs can actually be authenticated with when
nobody is watching. Claude Code's credential is a static bearer token — `claude setup-token`
mints one that lasts about a year, and `agentic-driver` injects it as `CLAUDE_CODE_OAUTH_TOKEN`
— so it fits a secret and a runner perfectly. Codex's does not. Its credential is
`auth.json`, a *file* holding an access token, a refresh token and an account id, which Codex
rewrites in place: sessions go stale after roughly eight days and the CLI persists new tokens
back into the file. OpenAI's own guidance says not to share that file across concurrent jobs or
multiple machines, and its refresh tokens are effectively single-use — once one copy refreshes,
the others are dead. Cross-machine reuse is an open, "not planned" defect.

That makes codex-in-CI unbuildable at the shape this feature needs. An ephemeral runner would
have to write a refreshed credential back into its own repository secret on every run, with the
previous value already invalid, and a panel that spawns two GPT reviewers concurrently violates
the one-file-one-process rule by construction.

## Considered options

**A self-hosted runner with a persistent `CODEX_HOME`.** Codex refreshes in place and the file
survives between jobs, which is the arrangement OpenAI actually recommends. Rejected on cost:
this ships to hobby and open-source repositories, where standing up hosts to review pull
requests is a bill that scales with the number of repos and buys nothing a laptop already has.
It also serialises GPT reviewers, so quorum stops working for exactly the provider it was
wanted for.

**A Claude-only panel in CI, GPT locally.** Buildable, and briefly the plan. Rejected because it
splits one feature across two execution models to rescue an automation nobody had asked for:
the review loop is driven by a person deciding a PR is ready, and a person with a terminal open
can type a command.

**API keys for both providers in CI.** Rejected outright. Metered billing for an always-on
reviewer is the cost this feature exists to avoid, and it is the reason GitHub's own Copilot
review was not simply switched on.

## Consequences

- "Automatic" means one command, not a push trigger. A local `pre-push` hook or a scheduled
  loop can fire it, and neither needs a runner or a secret.
- Nothing in a consumer repository holds a credential, so a fork, a clone or a leaked secret
  scan has nothing to find. The blast radius of the App key is one machine.
- The reviewer runs on ambient credentials, which means it inherits the operator's environment.
  Isolation is available and unused: `codex.WithConfigDir` sets a per-run `CODEX_HOME`, so a
  run can be pointed at its own credential directory rather than the operator's. Using it is a
  deliberate change rather than a default already in place.

  It does not rescue a concurrent quorum, which is the thing it looks like it would. Copying
  `auth.json` into N config directories produces N processes holding the same refresh token,
  and that token is effectively single-use — the first refresh invalidates it for the rest, so
  the copies destroy each other and the original. Per-run isolation separates where the
  credential is read from, not whose credential it is.
- A codex run therefore reports `MaxConcurrentRuns() == 1`, and a review schedules that
  provider's runs one at a time. Quorum on codex costs wall-clock rather than correctness; a
  provider with a static bearer token reports no limit and its runs go in parallel.
- A repository cannot be reviewed by someone who has not installed the App and `agtk`. Bot
  review is a property of the operator, not of the repository — which is the trade for it
  costing nothing to run.
