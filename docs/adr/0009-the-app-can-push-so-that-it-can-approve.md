# The App holds write access to contents so that its approval counts

GitHub weighs a pull request review by whether its author can push to the repository. A review
from an author who cannot is not weighed and found wanting — it is dropped out of the set the
decision is made from, so the pull request still reports that a review is required while an
approval sits on it reading `APPROVED`.

With `pull_requests: write` alone the App is exactly that author. It posts the approval, GitHub
records it, `authorCanPushToRepository` is false, `latestOpinionatedReviews` comes back empty
and `reviewDecision` stays `REVIEW_REQUIRED`. Nothing errors. So the App is granted
`contents: write`, which is what "can push" means, and every claim about approval in ADR 0006
and in CONTEXT.md rests on that grant rather than on the approval endpoint accepting the call.

The grant is wider than the use. ADR 0006 argues from blast radius that the installation token
reaches every repository the App is installed on, and this widens what that token can do from
commenting on pull requests to writing code in all of them. What keeps the difference honest is
a guard rather than a convention: no file under `internal/` names an endpoint that writes
contents, refs, trees, blobs or a merge, so the permission is held and never spent. Removing the
permission breaks approval silently and removing the guard breaks nothing visibly, which is why
the guard carries the explanation.

## Considered options

**Add the App to the ruleset's bypass actors.** Also unblocks the merge, and needs no
permission. Rejected because it is the opposite act: the App would be permitted to ignore the
requirement rather than to satisfy it, and a required approval nobody supplied is not a review
that happened.

**Accept that the approval does not count.** Rejected: an approval that no rule reads is a
comment with a green tick, and the subcommand's whole purpose is to satisfy a requirement a
solo author cannot satisfy alone.

## Consequences

- Push access is evaluated when the pull request is read, not when the review was submitted.
  An approval posted before the grant starts counting once it lands, and every approval the App
  has ever posted stops counting the moment the permission is removed. `agtk` therefore cannot
  treat a successful post as evidence that an approval counts, and does not claim it does.
- The permission has to be accepted on the installation as well as declared on the App. An
  installation still holding the old set posts approvals that silently do not count, which looks
  from the terminal exactly like one that does.
