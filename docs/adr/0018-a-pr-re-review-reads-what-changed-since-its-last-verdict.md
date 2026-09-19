# A pull request re-review reads what changed since its last verdict

A pull request the feature flow opens is reviewed, fixed and reviewed again until a pass finds
nothing, up to five passes. `agtk code-review run --pr` read the whole change every time:
merge-base to head, with the panel sized from all of it. A branch escalated to `deep` or
`deep-codex` by an auth path therefore paid for six runs over the whole diff on every pass, to
re-read code the previous pass had already found clean, so that a one-line fix could be checked.
Thread suppression kept the output from repeating itself. It did nothing for the cost, which was
the whole first pass again, each time.

The local loop already reads only what the previous pass's fixes changed. The pull request had
no equivalent, because two things depended on a posted review covering the whole change:

- approval read only the newest review of the head, and treated a finding missing from it as
  fixed;
- `Options.Base` was both where the diff started and where the manifest, prompts and convention
  documents were read (ADR 0007).

## Decision

**A re-review reads on from the last complete review, when that is safe.** `run --pr` finds the
newest review this installation posted that reached a verdict. When that review's head is an
ancestor of the pull request's head, and the base has not been merged in since (the merge base
of the base and that head is unchanged), the review reads that head..head. Otherwise it reads
the whole change, and says why. `--full` always reads the whole change.

**Only the diff moves.** `Options.Since` narrows the diff and the profile. The manifest, the
prompts and the conventions are still read at the merge base: `Since` is a commit on the branch
under review, and reading rules there would let a change write the rules it is judged by.
Comment placement still uses the whole change's added lines, because GitHub places a comment
against the pull request's own diff.

**The panel is sized from the delta.** Escalation matches what the delta touches. A fix to an
auth file still escalates, and a fix that touches only a README does not pay for `deep` again.
This is what the local loop already does on its later passes.

**The review marker records `since=`.** Approval walks from the head's review back through each
`since` to a review of the whole change. Every link has to be this installation's own, has to
have reached a verdict, and has to be a review of exactly the commit the next link names. If a
link is missing, approval refuses and asks for `--full`, because part of the change was read by
nobody.

**A carried finding is cleared by a statement, never by its absence.** A finding from an earlier
link that the head's review did not repeat is cleared when:
- it is marked a false positive, or
- it is answered by an account that can push, and its thread is resolved.

The head's review never read the code such a finding points at unless that code changed, so the
finding's absence from the head's review says nothing. The answer says what was done about it,
and the resolution is a person saying the conversation is over. A finding the head's review
repeats stays under the existing rule: fixed, or marked a false positive.

**The agent may mark; it may not resolve.** The feature flow's pull request loop runs unattended.
Its agent replies on every thread, and posts `agtk: false positive: <reason>` where it judges a
finding wrong. It never resolves a thread: resolving is the person's act. Approval of a carried
finding needs both, so a person still stands between the agent's claim and the gate.

## Rejected alternatives

- **A full re-review on every pass.** Correct and simple, but it costs the first pass again for
  every fix, and that cost is what stopped the loop being run at all.
- **A single `since=` with no chain.** Approval would read only the newest review, and every
  finding from before it would drop out of the gate unanswered.
- **Sizing the delta's panel from the whole change.** It keeps a second-model reading at full
  strength, but most of the saving is lost. It also asks a six-run panel to re-read a one-line
  diff.
- **The agent never marks false positives.** This keeps the False positive rule literal, but the
  loop can then never end clean on a finding the agent disagrees with, so every such finding
  waits for a person anyway. Resolution already gives the person the final word, at the point
  where it counts.

## Consequences

- A pull request's reviews are a chain rather than a single statement. `ReviewChain` is the one
  place that walks it, and `LastReview` stays the single selector for the head's own review, so
  suppression and approval cannot disagree about which review a head carries.
- A carried prompt-injection finding with no thread can only be lifted by `--full`, because no
  delta reads the code it names unless that code changed.
- Rewriting the branch, or merging the base in, costs one full review. That is the price of the
  narrower review being safe everywhere else.
- The False positive rule in CONTEXT.md is relaxed for an agent acting for a writer.
  Resolution, not marking, is now the act that only a person performs.
