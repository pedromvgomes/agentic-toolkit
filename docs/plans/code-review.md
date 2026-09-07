# Plan — bot code review on GitHub PRs

Status: designed, not started.
Created: 2026-09-07. Challenged: 2026-09-07.

The decisions that are hard to reverse live in `docs/adr/0005-reviews-run-locally-not-in-ci.md`,
`docs/adr/0006-the-judge-decides-agtk-transmits.md` and
`docs/adr/0007-untrusted-heads-are-closed-structurally.md`; the vocabulary lives in
`CONTEXT.md`.

`agtk code-review` runs a panel of reviewers over a change and posts what survives to a
GitHub PR as a bot. It replaces the `deep-code-review` and `pr-code-review` skills' fan-out
with one engine, driven from Go, so a local pre-push review and a PR review are the same
machinery pointed at different targets.

The existing skills are prior art we take content from, not a baseline we preserve. The
finding schema and the axis prompts survive nearly intact; the rung ladder does not, and
`## 6` says why.

---

## 1. The flow it serves

1. Plan, implement — a session.
2. Loop review and fix locally: `agtk code-review`, driven by the skill shell.
3. Push, open the PR.
4. `agtk code-review --pr N` — a panel runs, a **Review** is posted by the App.
5. `pr-review-resolver` fetches the comments and fixes them.
6. Back to 4, when the person decides — never automatically.

Every step runs on the operator's machine against CLIs they are already logged into. The
only credential is the App's private key, and it is machine-local.

## 2. The manifest

`.agents/code-review/manifest.yaml`, beside `.agents/memory/`, with `review.root` in the
entry manifest for the same reason `memory.root` exists. A repo with no manifest uses the
embedded default; a repo with one is using it **whole**, because prompt bodies stay
shareable through `builtin:` references rather than through a merge algorithm.

```yaml
version: 1

reviewers:
  unified:      {provider: claudecode, model: sonnet, prompt: builtin:unified}
  correctness:  {provider: claudecode, model: sonnet, prompt: builtin:correctness}
  security:     {provider: claudecode, model: opus,   prompt: builtin:security}
  performance:  {provider: claudecode, model: sonnet, prompt: builtin:performance}

judge:     {provider: claudecode, model: opus,   prompt: builtin:judge}
validator: {provider: claudecode, model: sonnet, prompt: builtin:validator}

panels:
  quick:    {reviewers: [unified]}
  standard: {reviewers: [correctness, security]}
  deep:     {reviewers: [correctness, security, performance], quorum: 2}

defaults:
  worktree: quick
  pr:       standard

escalate:
  - to: deep
    all:
      - touches: {matches: ["**/auth/**", "**/migrations/**"]}

  - to: deep
    any:
      - signals: {in: [concurrency, crypto, fix-revert]}

  - to: standard
    all:
      - changed_files: {gte: 20}
```

A consumer names its own reviewers and its own hot paths:

```yaml
reviewers:
  architecture: {provider: codex, model: gpt-5.4, prompt: ./prompts/architecture.md}
  performance:  {provider: claudecode, model: sonnet, prompt: builtin:performance}

escalate:
  - to: deep
    all:
      - touches: {matches: ["internal/resolver/**", "internal/stack/**"]}
```

## 3. Which panel runs

The **Context** picks a default; **Escalation** rules raise it. Rules never lower it, so a
misconfigured rule can cost money and can never produce a shallower review than the default.
Every rule is evaluated and the highest target wins, so rule order carries no meaning.

`agtk code-review --panel deep` overrides both. `agtk code-review --explain` prints the
default, every rule that fired, and the panel that resulted.

A condition is always `key: {operator: value}`. There is no bare form, so no combinator is
ever inferred:

| Key | Value | Operators |
|---|---|---|
| `touches` | globs over changed paths | `matches`, `not_matches` |
| `signals` | names from the built-in vocabulary | `in`, `not_in`, `all_in` |
| `changed_lines`, `changed_files` | integer, counted after mechanical exclusions | `gt`, `gte`, `lt`, `lte`, `eq` |

Each rule carries exactly one of `all:` or `any:`, named on the rule. A list inside an
operator means "any of", which the operator's own name forces; conjunction over a set is
`all_in`, and conjunction over globs is two members of `all:`.

Operators are words. `>=` opens a YAML folded block scalar and fails before the schema is
reached, with an error about header options that no amount of care could improve.

## 4. Signals

Built into the binary, language-aware, listed by `agtk code-review signals`. A repo does not
declare them: a signal the toolkit cannot detect is a gap to fill upstream where it is tested
across languages, exactly as ADR 0002 argues for driver providers. A repo's own escape hatch
is `touches`, which is honest about being path-only.

## 5. The pipeline

`agtk` captures the diff and the repo's convention docs **once**, from the base ref, and
injects them into every prompt. No reviewer re-derives them and no convention summarising
run exists — docs go in raw, because summarising rules before applying them costs a run to
become less accurate.

Then: reviewers → validators → judge → `agtk` posts.

- **Reviewers** run in parallel, `quorum` instances each. Agreement between instances is the
  confidence signal.
- **Validators** are per-panel, and forced on for a **Context** that posts. A false finding
  on a PR is published and blocks **Approval**.
- **The judge** merges, re-severities and decides what survives.
- **`agtk`** posts one review, `event: COMMENT`.

Every model run carries `Request.Schema`, so findings are schema-constrained by the driver.
A run that cannot produce the shape is an unmet constraint — a verdict carrying whatever
account exists, not a parse failure to retry, and not to be read as "found nothing".

Provider capability is asserted when the manifest is read, by type assertion on the driver's
interfaces. A reviewer asking for something its provider cannot do is refused before a
process starts rather than discovered by a failed run.

## 6. Considered and rejected

**The rung ladder.** `deep-code-review` sizes a review from size class, twelve criticality
signals, and measured reference fan-in. It exists because a *model* invokes that skill from a
session, where nobody chose a depth. Here a person types the command having just decided the
change is ready, so the ladder infers something the caller knows. What survives is the part
the caller genuinely forgets: escalation on paths.

**Reference fan-in.** The ladder's strongest input — `≥20 referencing files` forces the
deepest rung regardless of size. It needs per-language symbol extraction, and the skill gets
it by being in a session that can reach serena, which a binary cannot. Dropped rather than
approximated badly. `touches` recovers the intent by hand.

**Repo-declared content patterns.** Zones matching regexes against the diff were considered
so a repo could define `concurrency` itself. Rejected: a repo that writes `sync\.Mutex` gets
nothing the day it adds another language, and nobody goes back to update it.

**Nested condition combinators.** `any:`/`all:` blocks nesting freely, as Kyverno, Semgrep
and Cloud Custodian all allow. Rejected on Semgrep's own evidence — it shipped a structured
editor because "the large search space of ways YAML-writing can go wrong threatens to
undermine the commitment to making rule writing easy". One combinator per rule, and OR is a
second rule.

**Implicit AND across a rule's keys**, as Renovate's `packageRules`. Rejected because
nothing on the page says so, and the neighbouring list form would mean OR — two invisible
combinators in adjacent lines.

**Reviewers as a ninth definition category.** Rejected for ADR 0004's reason: it would make
`code-review` unusable on a repo that has not rendered, which includes the repo that just
adopted the toolkit.

## 7. Not yet designed

**Re-review deduplication.** The 4↔5 loop re-runs against a PR that already carries a bot
review. Findings already posted and addressed must not be raised again, which needs keying
to the head commit, reading back the bot's own threads and their resolution state, and
minimising superseded ones. This is the next design pass, before any posting code.
