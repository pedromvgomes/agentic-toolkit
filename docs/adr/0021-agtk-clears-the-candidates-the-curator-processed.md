# agtk clears the candidates the curator processed

A candidate is a finding waiting for a ruling, and a ruling ends with the file gone from
`candidates/`: promoted into a note, merged into one, or rejected. The completion report the
curator returns names each candidate it ruled on in `candidatesResolved`. The curator can delete
the file itself, through the `rm` grant of
`docs/adr/0004-the-curator-ships-in-the-binary.md`, but that grant is an absolute-path pattern on
the command line, so a model that spells the deletion `rm -f`, `git rm` or with a relative path
may not match it, and the provider's own approval policy can refuse a deletion the grant permits.
A run whose notes are all correct then leaves every ruled-on candidate staged, and the next run
rules on them again.

## Decision

**`agtk` clears, not the curator.** Once a run's completion report has passed verification,
`agtk` deletes every candidate the report names in `candidatesResolved` that is still in
`candidates/`, and prints each one it removed. A candidate the report resolves and the run
already deleted is skipped, so clearing after a run that deleted its own backlog removes
nothing. Nothing is cleared when verification fails, since the report is then contradicted by the
store and which candidates were ruled on is unsettled; a dry run clears nothing because it
writes nothing.

**The curator's `rm` grant stays.** Clearing does not supersede ADR 0004: the grant is still
constructed in Go, still bounded to `candidates/`, and the prompt still tells the curator to delete
what it resolves. Clearing is the deterministic backstop behind it, and a no-op whenever the
curator did the deletion itself.

**A candidate the run leaves unreported fails an unscoped run.** A run that names no notes, has
no limit, is not `--stale` and is not a dry run is the backlog job, which rules on every staged
candidate. One that ends with a candidate still staged and absent from the report is a candidate
nobody ruled on behind a run that reads as a finished pass, so it fails and lists them. The other
shapes are exempt because a candidate legitimately remains: a scoped or limited run is pointed at
some of the backlog, and a stale sweep never rules on candidates at all.

**An unreadable candidate fails the run and is never cleared.** A candidate whose frontmatter does
not parse is listed at the end of `curate`, fails the run, and is not counted among the unreported
leftovers, since the curator is told to leave it out of its report. Clearing skips it even where
the report names it: a file that cannot be read has no id the run can be trusted to have ruled on.
`agtk memory lint` reports each one with its file and
the parse error, because such a candidate is otherwise dropped from the backlog in silence.

**`Store.Lint` does not report them.** `verify` calls `Store.Lint` and the curator's prompt tells
it to run `agtk memory lint` on its own work and fix what it reports. The curator can only delete
candidates; it has no `Edit` grant on `candidates/`. An unreadable-candidate issue in that lint
would push it to delete the file, which either fails verification as an unreported removal or makes
the finding vanish. The check lives in `Store.LintCandidates`, which only the `lint` command calls.

## Considered options

**Leave deletion to the curator and diagnose the refusals.** Widen the `rm` grant patterns, or fail
verification when a resolved candidate is still staged. Rejected: the grant is one of several
places a deletion can be refused, and the run that fails verification over a leftover has already
spent a model call and left the store correct except for the leftover. The cause of a refusal is
the provider's and the model's, so a fix there is a fix for one spelling of one command.

**Have the curator retry the deletion, or run a second model call to clear.** Rejected by ADR 0002:
which candidates are done is already settled by the verified report, and deleting them is filing
rather than judgement. A model call on that path buys nothing that deleting a file does not.

## Consequences

- ADR 0002 holds. Clearing is a deterministic step after the one model call `curate` already makes,
  in the same package that constructs the driver, and no other command gains a path to one.
- ADR 0003 holds. Its rule is that `notes/` has one writer, the curator. Clearing writes nothing
  under `notes/`; it removes files from `candidates/`, which the explorer stages and the curator
  already deletes from.
- A report is trusted for which ids to remove only to the extent verification trusts it. Only ids
  that were in `candidates/` when the run started are removed, and they are filenames read out of
  that directory, so an id in the report that names a path anywhere else matches nothing and is
  never joined into a path to delete.
- A candidate the curator ruled on and `agtk` removed leaves no record in the run's own account, so
  `curate` prints what it cleared. A file vanishing from `candidates/` with nothing saying who
  took it reads as a lost finding.
- An unreadable candidate stays until a person repairs it, so a store holding one fails every
  `curate` run that would otherwise pass, and `lint` fails alongside it.
