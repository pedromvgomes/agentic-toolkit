---
about: the predicate deciding whether a review's failure counts as a Block must require every non-answering run to be blocked, not merely that some run was
saw:
  - internal/reviewrun/run.go
---

`Run`'s fallback trigger (`internal/reviewrun/run.go`) is `onlyBlocked(reports)`: it walks
every `RunReport`, skips the ones that answered, and returns `false` the moment it finds one
that neither answered nor was blocked — only a set where every non-answering run was blocked
returns `true`.

The first version of this used a plain `anyBlocked` (true the moment any report was blocked)
and it is the wrong predicate whenever a panel makes more than one run on the same axis or
role — quorum above 1, or several reviewers. One instance can be blocked on quota while a
sibling instance answers, and the judge can separately fail afterward for an ordinary reason
(a timeout, a bad schema). With `anyBlocked`, that review is marked `Blocked: true` and
`internal/cli/codereview_pr.go` withholds the PR post — even though the actual reason the
review is unavailable is the judge's unrelated, ordinary failure, which the codebase's
explicit invariant says must stay visible. `onlyBlocked` fixes this by requiring the whole set
of non-answering runs to agree.

This was not caught by the original test suite — `internal/reviewrun/fallback_test.go` only
covered "every run blocked" and "no run blocked", not the mixed case — it surfaced from an
adversarial panel review (`agtk code-review run`) as a RED finding. The regression test is
`TestABlockAlongsideAnOrdinaryFailureIsNotReportedAsBlocked` in the same file.
