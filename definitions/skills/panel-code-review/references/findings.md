# Presenting findings, and letting the user choose

One shape for every reviewed finding, whoever found it. A person who has read one of these
lists can read the other without relearning where the location is or how to say "fix that
one", and a finding does not change meaning because a different skill is showing it.

## The block

One block per finding, in this order. Omit a line whose value is absent rather than writing
"n/a" or "unknown".

```markdown
### 3 — Unparameterised SQL built from a request body
**RED** · `security:injection` · `src/UserController.kt:42-50`
**Found by:** security reviewer · 2 instances agreed · validator upheld

Request body fields are concatenated into the query string, so a crafted `name` changes the
statement rather than the value it binds.

> val q = "SELECT * FROM users WHERE name = '" + body.name + "'"

**Fix:** bind the value through a parameterised query instead of building the string.
```

- **The heading** is the number and a short title in plain words. Number **continuously across
  severities**, starting at 1, so "fix 1, 4 and 7" is unambiguous without naming a section.
- **The second line** is severity, category and location, in that order. The location is a
  path with a line or line range, in backticks, so it is clickable in a terminal.
- **`Found by:`** names who reported it, and any strength it carries — how many independent
  instances agreed, and what a validator concluded. A finding one reviewer reported once and a
  finding two reached independently are different claims, and the line is where that shows.
- **The body** is the problem in one or two sentences: what is wrong and what follows from it.
- **The quote** is the offending code, as a blockquote. A finding with nothing to quote is a
  finding without evidence.
- **`Fix:`** is a concrete action, not a direction to think about it.

Add **`Assessment:`** immediately before `Fix:` when the finding is somebody else's claim
rather than this run's — a reviewer's comment being triaged. It is one of `Valid concern`,
`Partially valid`, `Not applicable` or `Already addressed`, followed by why. A finding that
arrived already validated has no assessment line: re-judging it would be a third opinion on
top of the two it already carries.

## Order and grouping

Sort RED, then AMBER, then GREEN; within a severity, by path. Head each severity that has any
findings with `## RED — must fix`, `## AMBER — should fix`, `## GREEN — worth considering`,
and omit a severity entirely when it is empty. Numbering runs straight through the headings
and never restarts.

Group findings that are really one underlying issue into a single block naming every location,
rather than repeating a block per site.

## Closing

After the blocks, two short sections:

```markdown
## What's good
- <2-4 real observations. Omit the section rather than pad it.>

## Record
<panel and reviewers run> · <what validation dropped> · <conventions applied> · <cost>
```

Then the choice, in these words, so the grammar is the same wherever findings are shown:

> Tell me which to fix: **all**, **all RED**, **1, 4, 7**, **all except 2**, or **none**.

Nothing is edited without an explicit selection. "None" is a complete answer and ends the
review without further offers.

## When there is nothing to show

Say the review found nothing, print `What's good` and `Record`, and stop. Do not print empty
severity headings, and do not ask which of no findings to fix.
