# The review skill is a shell over the engine, and holds no analysis of its own

One skill drives `agtk code-review`. It resolves what to review, reports what the panel
would cost before spending it, runs the engine, presents what came back, and routes fixing.
It sizes nothing, prompts nobody, validates nothing and posts nothing itself, because the
binary does all four in Go and under test.

A skill and a binary that both know how to review are not redundancy, they are two answers
that disagree the first time either changes. Every row where they overlapped had the same
shape: one side computed and tested, the other prose a model re-derived per session. The
posting row was worse than duplication — a review posted by the skill carried no **Review
marker**, so `agtk code-review approve` read that head as never reviewed.

## Considered options

**A skill per target: one for a branch, one for a pull request.** This is the shape that
existed, and the pull-request skill declared `requires: skills/deep-code-review`. Rejected
because the engine already unifies the targets: `--pr` decides base, head and **Context**
together, so a skill per target is a second place the target is decided, and the second place
is the one that drifts. The seam between the two skills existed only to hand a diff across
it, and every flag that seam carried — whether the head's code was available, whether the
head was untrusted, where full-file context lived — is a structural property of the binary
under ADR 0007 rather than something to pass.

**Promoting the language prompt bodies to `builtin:`.** The skill carried Go, Kotlin/Spring,
React and Rust sets, and they were not filler: the Go security body names `text/template`
against `html/template`, `hmac.Equal`, `http.MaxBytesReader`. Rejected for the reason
`internal/reviewrun/prompt.go` already gives — a stack prompt inside the binary is one the
toolkit owes every repo writing that language, forever, and a repo that has the language can
write a body that knows its own stack. Shipping them as bodies a consumer copies into its
**Review manifest** was weighed and also rejected: they would be the toolkit's to maintain
in everything but name.

The four `shared/` bodies were not a trade-off at all. Comment hygiene and the
repo-conventions rule are already in `internal/reviewrun/prompts/correctness.md`, and the
severity calibration and evidence rule are in the reviewer preamble.

**A skill that can approve.** Rejected. Approval is four conditions with no override, and
the one thing it must never be reachable from is a model that just reviewed the code. The
skill states that it does not approve, and does not name the subcommand that does.

## Consequences

- A path target and user-chosen exclusions are gone. `run` takes `--base` and `--head` and no
  pathspec, and `internal/review/exclude.go` decides what is not worth reviewing. "Review
  just this directory" has no engine equivalent.
- The four language prompt sets are deleted rather than relocated. A Go repo reviewed by this
  toolkit gets no prompt that knows Go. That is the cost of the call `prompt.go` records, paid
  where it was always going to be paid.
- `explain` gains `--pr`, so the panel a pull request would get can be reported before a panel
  runs. It stays model-free, and ADR 0002 is untouched, but it is no longer true that every
  subcommand but `run` is safe on the path of a hook: `explain --pr` reads GitHub and needs
  the **App registration**. Bare `explain` is unchanged.
- The finding presentation is one file, symlinked into `pr-review-resolver`, because two
  skills that show findings differently make one review look like two. Rendering dereferences
  the link, so a consumer receives a real file. A checkout without symlink support does not:
  git materialises the link as a text file holding its own target path, and that is what would
  ship. The failure is visible in the rendered skill rather than silent.
- Fixing a pull request's findings is `pr-review-resolver`'s alone. The engine's findings
  arrive on the pull request as **Comment thread**s, and answering a thread is what
  **Approval** later requires; code fixed without a reply leaves the gate unsatisfiable.
