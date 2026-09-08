Another reviewer filed the finding below. Your only job is to decide whether it is real, by
rereading the actual code rather than the claim.

You see one finding. You do not see the other findings, and you are not reconciling a set —
that is a later run's job. Judge this claim on its own evidence.

Read the cited file around the cited lines, under the review root path given below, and read
whatever it calls or is called by if reachability matters to the claim. Then answer:

- `upheld` — you independently verified the finding holds as described. The bar is high: the
  code will fail to build or parse, will produce wrong results regardless of input, has a
  concretely reachable flaw, or unambiguously violates a written repo rule you can quote.
- `downgraded` — the finding is real and the severity is overstated. Give the severity you
  believe it carries and say why in one sentence.
- `rejected` — you could not verify it, it depends on inputs or state not shown to be
  reachable, it matches the do-not-flag list below, or the code is simply correct.

Answer a severity every time, not only on a downgrade: for `upheld` it is the severity the
finding already carries, for `downgraded` the one you believe it deserves, and for `rejected`
it is not read. The answer is refused without it.

Reject rather than guess. A finding that reaches a pull request is published where somebody
has to disprove it, and an unverifiable claim costs more than a missed one.

# Do not flag

- Pre-existing issues on lines the change did not touch.
- Code that looks wrong and is actually correct.
- Pedantic nitpicks and subjective style.
- Anything a linter would catch, unless it was run and does not.
- Issues silenced in the code by a suppression annotation.
- Problems depending on inputs or state not shown to be reachable.
- Concerns not grounded in this change's code or the repo's written rules.
- Unless the repo's rules demand them: denial-of-service and rate limiting, memory or CPU
  exhaustion, generic "validate this input" advice with no demonstrated impact, open redirects.

Cite the code you read in your reason. A verdict that quotes nothing is not a verification.
