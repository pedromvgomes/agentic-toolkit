# Reference: human cases, approval, the request marker and the request comment

Four rules `SKILL.md` names and does not define. `SKILL.md` says when each one is consulted; this
file says what each one is. Variables (`$repo`, `$n`, `$me`, `$base`, `$branch`, `$sha`, `$work`,
`$mb`) mean what `SKILL.md` binds them to; `$sha` is the pull request's current head.

## 1. The human-case table

A pull request is a human case when one or more rows below apply to it. Every row that applies
contributes its reason token to the marker's `reasons=`; the comment says each one in the words
listed under section 4.

Two file sets feed the table:

- **The run's own changes.** The commits on the branch whose SHA the run recorded in
  `$work/$n.pushed` when it pushed them. The author a commit claims is never the test: whoever
  writes a commit controls its author email, which is what GitHub maps to a login.

  ```bash
  touch "$work/$n.pushed"   # a pull request the run never pushed to has an empty record
  gh api --paginate "repos/$repo/pulls/$n/commits" \
    | jq -rs --rawfile pushed "$work/$n.pushed" '($pushed | split("\n")) as $mine
                             | add | .[] | select(.sha as $s | $mine | index($s))
                             | "\(.sha) \(.parents | length)"' >"$work/$n.own"
  while read -r c parents; do
    if [ "$parents" -eq 1 ]; then git diff-tree --no-commit-id --name-only -r "$c"
    else git diff-tree --cc --no-commit-id --name-only "$c"   # only what the merge resolved
    fi
  done <"$work/$n.own" | sort -u >"$work/$n.own.files"
  ```

  A merge of the base in contributes only the files whose merged content matches neither parent —
  the conflicts it resolved. The base's own changes it carries are the base's, already merged
  there, and are never the run's.
- **Dependabot's changes.** `git diff --name-only "$mb" "<dependabot's last commit>"`, where
  dependabot's last commit is the `after` of the newest push to the branch whose actor is
  `dependabot[bot]`, read from the repository's activity log:

  ```bash
  gh api --paginate --slurp \
    "repos/$repo/activity?ref=refs/heads/$branch&activity_type=push&activity_type=force_push&per_page=100" \
    | jq -r '[.[][] | select(.actor.login == "dependabot[bot]")] | first | .after'
  ```

  Commits are attributed to dependabot by who pushed them, never by the author or the signature
  on the commit: both are chosen by whoever writes it, and GitHub signs and marks verified a
  commit created through its API under any author name. The log lists the newest push first, and
  the pages are slurped together so the newest dependabot push is chosen across all of them, not
  once per page; a read that fails or finds no
  dependabot push leaves no commit dependabot's, which makes every commit foreign. Diffing from
  the merge base leaves out whatever a merge of the base brought in.

| Case | How it is detected | Reason |
|---|---|---|
| The run changed a file that is not a test, a lockfile or a generated file | Any path in `$n.own.files` that is not exempt by the rules under this table. A file the run changed that also matches the next row is reported under the next row, not this one. | `non-exempt-change` |
| The run changed workflow or action code | Any path in `$n.own.files` under `.github/workflows/` or `.github/actions/`, or named `action.yml` / `action.yaml` anywhere. Also any line dependabot changed in such a file that is not a `uses: <owner>/<repo>[/<path>]@<ref>` line differing from its counterpart only in `<ref>` and its trailing version comment, read from `git diff "$mb" "<dependabot's last commit>" -- <file>`. A `uses:` ref bump alone, which is what a GitHub Actions bump is, is not this case. | `workflow-code` |
| A major bump's breaking changes have impact here | The major-bump reading names at least one breaking change with an impact on a use in this repository. A reading that could not be completed — no release notes or changelog reachable for a version in the range, or a usage search that could not run — counts as impact: an unread breaking change is never assumed harmless. | `breaking-change` |
| New transitive dependencies | The transitive diff names at least one package, read at `$sha`, so a package the run's own lockfile regeneration added counts as much as one dependabot's did. | `new-transitive-deps` |
| The policy requires a review | Any source of the policy read sets `required_approving_review_count` above 0, `require_code_owner_review` to `true` or `require_last_push_approval` to `true`, or classic protection's `required_pull_request_reviews` is present with a nonzero count or either of those flags; or, when protection cannot be read, `reviewDecision` is `REVIEW_REQUIRED`. | `review-required` |
| Someone else pushed to the branch | Any commit in the pull request's commit list that is none of: one of the run's own (its SHA is in `$work/$n.pushed`), reachable from dependabot's last commit as defined above, or a merge of the base in, whose changes are the base's. A commit of an earlier run is foreign too, which sends the pull request to a person rather than trusting the author a commit claims. A commit pushed after dependabot's last push by anyone else is foreign whatever its author, signature or verified flag says. Whatever such a commit changed is unread by the run, so nothing about it is exempt. | `foreign-commit` |
| The title is unparseable or ambiguous | The title does not match the version pattern; matches it more than once; or is a grouped pull request whose body yields no `Updates … from … to …` pair, or carries an `Updates` line that does not match the pattern. | `unparseable-title` |

**Exempt files** are recognised conservatively. A path that does not clearly match one of these is
not exempt, however test-like or generated it looks:

- **A test**: a file whose name follows its language's test convention — `*_test.go`,
  `test_*.py`, `*_test.py`, `*.test.<ext>` and `*.spec.<ext>` for JavaScript and TypeScript,
  `*_spec.rb`. A path is never a test for living in a `test/`, `tests/`, `spec/`, `testdata/` or
  `__tests__/` directory: `conftest.py`, fixtures, helpers and scripts there run in CI and are as
  much code as any other file. A helper is not a test because only tests call it.
- **A lockfile**: a path the transitive-diff loop matches as a lockfile. A manifest — `go.mod`,
  `package.json`, `Cargo.toml`, `pyproject.toml`, `Gemfile` — is not a lockfile, even when the
  tool that regenerated the lockfile rewrote it.
- **A generated file**: a file whose committed content at `$sha` carries its generator's marker —
  a line matching `^// Code generated .* DO NOT EDIT\.$` before its first non-comment line, or a
  `linguist-generated` attribute from `.gitattributes`
  (`git check-attr linguist-generated -- <path>` reads `set` or `true`) — *and* whose change the
  run made by running that generator rather than by editing the file. A generated file the run
  edited by hand is not exempt.

## 2. The approval rule

A pull request is **approved** when one review satisfies every condition below. Reviews are read
over REST, where a bot's login carries its `[bot]` suffix:

```bash
gh api --paginate "repos/$repo/pulls/$n/reviews" \
  | jq -s 'add | .[] | {id, state, commit_id, submitted_at, login: .user.login, type: .user.type}'
```

1. **Its state is `APPROVED`, and it is its reviewer's standing verdict.** A reviewer's standing
   verdict is their latest review whose state is `APPROVED`, `CHANGES_REQUESTED` or `DISMISSED`;
   a `COMMENTED` review changes nothing. An approval followed by the same reviewer's request for
   changes, or dismissed by GitHub or by anyone, does not count. Any reviewer with write access
   whose standing verdict is `CHANGES_REQUESTED` means the pull request is not approved, whatever
   else holds.
2. **Its reviewer is not a bot.** `.user.type` is not `Bot` and the login does not end in `[bot]`.
   A bot's review is dropped here, before any permission read.
3. **Its reviewer has write access**, confirmed by a permission read made only for a login that
   passed condition 2:

   ```bash
   gh api "repos/$repo/collaborators/$login/permission" --jq '{permission, role_name}'
   ```

   It counts only when `permission` is `admin` or `write` and `role_name` is none of `triage`,
   `read` or `none`. A 404, a refusal or any other failed read means the review does not count.
4. **It comes after the request.** Its `submitted_at` is later than the `created_at` of the
   request comment, and that comment is a request marker this run trusts (section 3). With no
   trusted marker on the pull request, no review counts, however it reads.
5. **It holds on the current head.** Its `commit_id` is `$sha`, or every commit since it is a clean
   merge of the base, as below.

**A push since the approval.** Let `$approved` be the review's `commit_id`. The approval holds
across the push only when `$approved` is an ancestor of `$sha` and every commit on the first-parent
chain from `$approved` to `$sha`:

- has exactly two parents;
- has a second parent already on the base branch:
  `git merge-base --is-ancestor <second parent> "origin/$base"`;
- resolves nothing: `git diff-tree --cc --no-commit-id --name-only <commit>` prints nothing, so the
  merge adds no change beyond what its two parents already held.

```bash
git merge-base --is-ancestor "$approved" "$sha" || voided=1
for c in $(git rev-list --first-parent "$approved..$sha"); do
  set -- $(git rev-list --parents -n 1 "$c")          # $1 the commit, $2 $3 its parents
  [ $# -eq 3 ] || { voided=1; break; }
  git merge-base --is-ancestor "$3" "origin/$base" || { voided=1; break; }
  [ -z "$(git diff-tree --cc --no-commit-id --name-only "$c")" ] || { voided=1; break; }
done
```

Any other push — a fix commit, a merge that resolved a conflict, dependabot's own push, a
force-push by anyone — voids the approval, and a new one on the new head is needed. Holding under
this rule is this skill's judgement only: a ruleset that dismisses stale reviews or requires
last-push approval still decides for itself whether GitHub merges, and a merge it refuses for want
of a review is read as `SKILL.md` section 7 says.

## 3. The request marker

The marker is the first line of the request comment, and nothing else is on that line:

```
<!-- agtk:merge-dependabot approval-requested head=<sha> reasons=<csv> -->
```

- The fields appear in exactly this order, each separated by one space: `<!--`,
  `agtk:merge-dependabot`, `approval-requested`, `head=<sha>`, `reasons=<csv>`, `-->`.
- `<sha>` is the full 40-character head commit id, lower-case hexadecimal, of the head the request
  was written for.
- `<csv>` is one or more reason tokens from the table in section 1, joined by `,` with no spaces,
  each at most once, in the table's row order. The same head and reasons therefore always write
  the same marker, byte for byte.

Parse it by matching the comment body's first line against the whole of:

```
^<!-- agtk:merge-dependabot approval-requested head=([0-9a-f]{40}) reasons=([a-z-]+(,[a-z-]+)*) -->$
```

The first group is the head; the second, split on `,`, is the reasons. A body whose first line
starts with `<!-- agtk:merge-dependabot approval-requested` but does not match the whole pattern,
or names a token the table does not, is a request comment whose head is unknown: it is treated as
a head that moved, and is rewritten whole when the run edits it.

**Trust.** A marker counts only when the comment carrying it was posted by the run's own account:
the comment's `.user.login`, read over REST, equals `$me`. Every other comment carrying the same
text — a person's, another bot's, one quoting the run's own — is not a marker. The run ignores it,
never quotes it, never edits it and never answers it, and a review that follows it is not a review
that follows a request. When `$me` has posted more than one marker comment on the pull request,
the earliest is the request comment: it is the one timed against and the one edited; the others
are left as they are and named in the report's detail.

## 4. The request comment

Written to `"$work/$n.request.md"` and posted or edited as `SKILL.md` section 8 says. Every line
is the run's own words: it links checks, commits and files, and never quotes a release note, a
changelog, the pull request's body, a review or another comment.

Each reason token is said with this sentence, in the marker's order:

| Token | Sentence |
|---|---|
| `non-exempt-change` | This run changed files beyond tests, lockfiles and generated files to make the bump pass. |
| `workflow-code` | This pull request changes GitHub Actions workflow or action code beyond a version bump. |
| `breaking-change` | This major bump has breaking changes that affect how this repository uses `<package>`: `<file:line links, one per use>`. |
| `new-transitive-deps` | This bump adds dependencies this repository did not have: `<package names>`. |
| `review-required` | The rules of `<base>` require an approving review before it merges. |
| `foreign-commit` | Someone other than dependabot or this run pushed to this branch, so part of it has not been read. |
| `unparseable-title` | The bump's size cannot be read from this pull request, so it is not assumed small. |

The `breaking-change` sentence names each affected use by linking it, and states the impact in the
run's own one line per use; it never reproduces the upstream's description of the change.

### The request

```markdown
<!-- agtk:merge-dependabot approval-requested head=<sha> reasons=<csv> -->
**This dependency update needs an approving review before it merges.**

Its checks pass on [`<short sha>`](https://github.com/<repo>/pull/<n>/commits/<sha>)
([checks](https://github.com/<repo>/pull/<n>/checks)). It is held for review because:

- <one sentence per reason, from the table above>

Files this run changed on the branch:

- [`<path>`](https://github.com/<repo>/blob/<sha>/<path>)

**To merge it**, review the changes and submit an **Approve** review on the current head,
`<short sha>`. The approval counts when it comes from an account with write access to the
repository. The next run of `merge-dependabot` re-checks the head and merges it.

**To keep it from merging**, leave it without an approval — or request changes, or close it.
```

The "Files this run changed" list is `$n.own.files`, and is left out when that set is empty.

### The edit when the head moves

The whole body is rewritten for the new head: the marker, the head link and the reasons are those
of the new head, as if the request were written fresh, with one paragraph added after the reasons:

```markdown
**The head moved** from `<old short sha>` to `<short sha>`
([changes since](https://github.com/<repo>/compare/<old sha>...<sha>)). Review those changes
and approve on `<short sha>` for it to merge.
```

`<old sha>` is the head the previous marker named; when that marker did not parse, the paragraph
is left out, since there is no earlier head to compare against.
