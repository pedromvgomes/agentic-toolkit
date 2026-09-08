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
- Approval is granted only when a review exists for the PR's current head commit and reached a
  verdict, every finding it reports at or above the configured severity floor is marked a false
  positive, and no comment thread on the pull request is unresolved. A
  `security:prompt-injection` finding agtk could not attach to the pull request blocks
  regardless of all of it.
- Nothing overrides the gate. There is no `--force`, because a flag that approves anyway makes
  every clause above a checklist rather than a control, and the operator who would type it is
  the one person the gate exists to slow down. Both ways past a finding are acts on the pull
  request instead: change the code, so the evidence changes and the next review does not report
  it; or reply on its thread saying it is not a defect. Each is attributable to an account,
  visible to anyone reading the change, and reversible.
- A false positive may be marked only by an account with write access. The author of a change
  is the party a review does not trust, and a finding its own author could dismiss is one an
  injected instruction can dismiss too.
- Approval reads what the last review found out of that review's own body, from a marker naming
  the commit, the verdict and every finding by fingerprint and severity. Nothing is persisted
  between runs and the pull request is the only record, so the alternatives were to re-derive
  the review — which approval must not do, since it requires a review to *exist* rather than to
  run one — or to read severity out of rendered prose, which makes a rendering change a silent
  approval bug.
- A review posts one call; approval is a second command that makes another. A finding stated in
  the review body carries no thread until agtk attaches one, which is one further request per
  such finding, and GitHub refuses one whose path the change does not touch. So "exactly one API
  call" holds for the review proper and not for the file-level threads that follow it: one that
  fails leaves a finding in the body, which is reported rather than lost.
