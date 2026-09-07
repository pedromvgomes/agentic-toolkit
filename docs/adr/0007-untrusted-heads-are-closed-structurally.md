# A reviewed head is closed off by where the child runs, not by what it is told

Everything on the branch under review is written by its author, who may not be trusted, and
that includes the files a coding-agent CLI reads as instructions before it ever sees a prompt.
Three things close that, none of them prose:

1. **`agtk` reads the review manifest, the reviewer prompts and the convention docs from the
   base ref.** It reads them itself and passes their bodies to the run, so a branch cannot
   rewrite the rules its own change is judged against, and the CLI's own discovery is never
   what supplies them.
2. **No reviewer runs with the reviewed code as its working directory.** The head is exposed as
   a detached worktree — the review root — outside the project directory, and the child's
   working directory is elsewhere. Files under the review root are read as material.
3. **Instruction filenames inside the review root are neutralised before any child starts.**

The third exists because the second is not sufficient for every provider. Claude Code can be
told to load no settings at all, and `agentic-driver` puts `--setting-sources ""` first in every
argv it builds — the same refusal ADR 0004 relies on to close `apiKeyHelper`. Codex has no
equivalent. It walks from the project root down to the working directory reading
`AGENTS.override.md`, then `AGENTS.md`, then `TEAM_GUIDE.md` and `.agents.md`, and it takes
project-level `.codex/config.toml` into its configuration precedence. There is no flag that
turns that off. A head carrying `AGENTS.override.md` would therefore be read as instructions
ranking *above* the repository's real `AGENTS.md`.

Codex narrows the options further. Its `PermissionArgs` refuses a per-tool allowlist outright —
the CLI has none, and accepting one could only mean discarding it — so a codex run is bounded by
a sandbox mode and nothing finer. The grant that ADR 0004 constructs in Go to bound the curator
has no codex equivalent, which is why what bounds a reviewer here is where it runs and what that
directory holds, rather than a list of tools it may call.

Approval is what raises the stakes. A review that reports nothing reads as a clean review, and
a clean review is what unblocks approval — so an injected instruction that suppresses findings
converts into a green light a person signs off. Reviewers therefore report an imperative
addressed to the reviewer as a finding (`security:prompt-injection`), and such a finding blocks
approval regardless of the configured severity floor.

## Considered options

**Instruct the reviewers and trust the instruction.** Rejected as the whole defence: discovery
happens before the prompt is read, so no wording in the prompt can prevent it. The instruction
is kept as the innermost layer, not the outermost.

**Review the diff alone and never check the head out.** Closes discovery completely, and the
review engine already supports it. Rejected as the default because reviewers lose full-file
context, which is most of what separates this from reading a patch.

## Consequences

- The neutralised filename list is a denylist, and denylists rot: a vendor that adds a
  discovery filename opens the hole again silently. The neutral working directory sits behind
  it precisely so a missed name is not on its own sufficient.
- Convention docs that exist only on the head are skipped rather than read, so a change that
  introduces a rule is not judged against it.
- A reviewer that cannot read the repository's real `AGENTS.md` through discovery gets it in
  its prompt instead, from the base ref, which is the only copy it should ever have seen.
