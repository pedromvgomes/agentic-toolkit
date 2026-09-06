# The judge decides what a review says; agtk alone talks to GitHub

No model in a review run holds a GitHub credential. Reviewers emit findings as JSON, the judge
reads every reviewer's output and returns the findings that survive — merged, re-severitied,
deduplicated — and `agtk` turns that JSON into exactly one API call: create a review with
`event: COMMENT`. Approval is a separate subcommand a person types, and it is not reachable
from a review run's code path at all.

This is ADR 0003's rule in a second place. There, the curator holds authority over what a note
says and `agtk memory anchor` performs the write; here the judge holds authority over what the
review says and `agtk` performs the post. Splitting judgment from transmission is what makes
the guarantee structural: the bot cannot approve, merge, close or push because no code exists
that would, not because a prompt was told not to.

The blast radius argues the same way. The App installation token reaches every repository the
App is installed on, and a review run is driven by a model reading a diff written by someone
else. Handing that process the credential widens a grant across an entire account to save a
JSON schema. The credential also does not arrive by accident: reviewers run on ambient
credentials and inherit the operator's environment, so passing the GitHub token would be a
deliberate construction, not an omission.

Three mechanical reasons reinforce it. Inline comment positions must land on the diff of the
PR's head commit or GitHub rejects the whole review with a 422, which is deterministic work
that belongs in tested Go. A panel that spent four model runs should not repeat them because a
POST was rate-limited. And skipping findings already posted and addressed, which is what makes
the review-and-fix loop bearable, means reading existing threads — free once `agtk` owns the
API surface, and awkward from inside a prompt.

## Considered options

**Grant the run `gh` and let it post.** Fewer moving parts, and the model can phrase each
comment where it decides it. Rejected: it makes "the bot never merges" a sentence rather than a
property, and every argument above then rests on the model having read that sentence.

**Let the judge approve when it finds nothing.** Rejected. A run that can approve the code it
just reviewed is precisely the hazard GitHub blocks `GITHUB_TOKEN` approvals to prevent, and it
is the one place where an injected instruction in a diff would convert directly into a merge.

## Consequences

- Findings are schema-constrained at the driver rather than validated after the fact. Reviewer
  runs and the judge run each carry a `Request.Schema`, and the answer arrives in
  `Result.Structured`. A run that cannot produce the shape reports an *unmet constraint* —
  `IsError` set, `Structured` nil, and `Text` carrying whatever account exists — which is a
  verdict to surface, not a parse failure to retry. A sandbox refusal on a constrained run
  reports the same way, so a reviewer denied the authority it needed is not mistaken for one
  that found nothing.
- The judge writes comment bodies and severities and does not lose expressiveness; it loses
  only the ability to transmit them.
- `--dry-run` costs nothing to build, because rendering and posting are already separate.
- Approval is granted only when a review exists for the PR's current head commit and nothing at
  or above the configured severity floor survived. `--force` overrides that and says so in the
  approval body; a `security:prompt-injection` finding blocks approval regardless of the floor.
- The floor makes the gate a checklist rather than a control: an operator who can type
  `--force` can always approve. It catches a head that was never reviewed, not a person who
  decided not to review.
