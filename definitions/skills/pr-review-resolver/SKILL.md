---
name: pr-review-resolver
description: |
  Analyze and resolve PR review comments — from GitHub Copilot, CodeRabbit, human reviewers, or any other source.
  Use this skill whenever the user wants to address PR review feedback, fix review comments, handle Copilot suggestions,
  respond to code review findings, or resolve PR conversations. Trigger on phrases like "address PR comments",
  "fix review feedback", "handle copilot review", "resolve PR findings", or when a PR URL is provided with
  intent to address its review comments. Also use when the user says "fix PR comments", "address review",
  or references open review threads they want to resolve.
---

# PR Review Resolver

Analyze PR review comments, present a structured assessment to the user, implement approved fixes, and close
the feedback loop by responding to reviewers.

## Workflow

### Phase 1: Gather Review Comments

1. **Identify the PR** — the user may provide a PR URL, a PR number, or you may need to detect it from
   the current branch. Use `gh pr view` or the GitHub MCP tools to find the PR.

2. **Fetch all review comments** — collect every unresolved review thread on the PR. The
   threads come from GraphQL, because that is the only place `isResolved` is exposed:

   ```bash
   gh api graphql -F owner='{owner}' -F repo='{repo}' -F number=<PR> -f query='
     query($owner: String!, $repo: String!, $number: Int!) {
       repository(owner: $owner, name: $repo) {
         pullRequest(number: $number) {
           reviewThreads(first: 100) {
             nodes {
               id isResolved isOutdated path line
               comments(first: 50) { nodes { databaseId author { login } body url } }
             }
           }
         }
       }
     }'
   ```

   Skip threads where `isResolved` is true. For each remaining thread, capture:
   - The **root comment's `databaseId`**: the first node under `comments`. Your reply is
     posted against this ID in Phase 5, so record it next to the finding now. GitHub can only
     reply to a thread's root comment, not to a reply inside it.
   - The reviewer (human or bot — e.g., `copilot`, `coderabbitai`, a team member's handle)
   - The file and line range the comment targets
   - The full comment text, including any suggested code changes
   - The rest of the thread (replies, if any) for context

   A review's summary body and a comment on the PR's conversation tab have no thread and no
   `databaseId` you can reply against. Capture them too, but mark them **conversation** rather
   than **thread**. That mark decides where the reply goes in Phase 5.

3. **Read the relevant code** — for each comment, read the file and surrounding context so you can
   form an informed opinion. Don't just rely on the diff — understand the broader context of the code.

### Phase 2: Analyze and Present Findings

Present the comments as findings, in the shape `references/findings.md` prescribes — read that
file before writing the report. Every reviewed finding a user sees uses it, whoever found it, so
someone who has read one such list can read this one without relearning where the location is or
how to say "fix that one".

Each comment maps onto a block: the reviewer's handle on the `Found by:` line, the file and line
range as the location, the reviewer's own words as the quote, your explanation of what they are
flagging as the body, and your concrete approach as `Fix:`. Because these are somebody else's
claims rather than findings that arrived already validated, every block carries the
`Assessment:` line — `Valid concern`, `Partially valid`, `Not applicable` or `Already
addressed`, with why.

Severity is your judgement of the underlying concern, not the reviewer's tone: what must be
fixed is RED, what should be is AMBER, and a nit is GREEN. A comment you assess as `Not
applicable` still gets a block — the user needs to see it to disagree with you — and its
severity is the one the concern would carry if it held.

Present **all findings at once**, and close with the choice that file gives, so the selection
grammar is the same wherever findings are shown.

**Important considerations when analyzing:**
- Be honest in your assessment — don't rubber-stamp every comment as valid. Some automated reviewers
  produce false positives or flag things that are intentional design choices.
- When a comment is not valid, explain clearly why — the user needs to understand your reasoning
  to make a good decision, and you'll need to articulate this in the reply to the reviewer.
- When a comment is valid, think about the best fix — not just the quickest. Consider whether the
  reviewer's suggested change is the right approach or if there's a better alternative.
- Group related comments that touch the same concern — sometimes multiple comments are really about
  one underlying issue.

### Phase 3: Plan the Fixes

Once the user confirms which findings to address:

1. **Enter plan mode** — before writing any code, create a detailed implementation plan covering all
   approved fixes. The plan should include:
   - Each finding being addressed, with its number and title
   - The specific files and locations that will be modified
   - The concrete changes to be made (what code will be added, removed, or modified)
   - How fixes that interact with each other (e.g., two comments about the same function) will be
     coordinated
   - Any potential risks or side effects of the changes

2. **Present the plan for approval** — show the complete plan to the user and wait for explicit approval
   before proceeding to implementation. The user may request adjustments to the plan — iterate until
   they're satisfied.

   This checkpoint exists because the cost of implementing the wrong fix is much higher than the cost
   of reviewing a plan. The user knows the codebase context that you might not — let them catch issues
   before code is written.

3. **Do not proceed to implementation until the user approves the plan.** If the user requests changes
   to the plan, update it and present the revised version for approval.

### Phase 4: Implement Approved Fixes

Once the plan is approved:

1. **Implement the fixes** — make the code changes exactly as outlined in the approved plan. Follow the
   project's existing conventions and patterns. Read surrounding code to match style.

2. **Verify the changes** — run relevant tests or builds to make sure the fixes don't break anything.
   Use the project's standard test commands.

### Phase 5: Commit, Push, and Respond

1. **Commit** — create a single commit with all fixes. The commit message should:
   - Summarize what was fixed at a high level in the subject line
   - In the body, include a detailed report:
     - Which review findings were addressed (reference them by number)
     - What was changed for each
     - How each change addresses the reviewer's concern

   Example:
   ```
   fix: address PR review feedback

   Resolved the following review findings:

   - Finding 1 (null safety): Added null check in UserService.findById()
     before accessing the response body. This prevents the potential NPE
     flagged by @copilot when the upstream service returns 204.

   - Finding 3 (error handling): Replaced generic catch block with specific
     exception types in OrderProcessor. This ensures transient failures
     are retried while validation errors fail fast, as suggested by @reviewer.
   ```

2. **Push** — push the changes to the PR branch.

3. **Reply to reviewers** — every finding the user saw in Phase 2 gets a reply, whether or
   not it was fixed. That includes findings the user chose to leave alone. The job isn't done
   while any of them is still unanswered.

   For an addressed comment, the reply says:
   - What was changed, and in which commit (give its SHA)
   - How the change addresses the concern
   - Whether the reviewer has any further feedback

   For a comment that was intentionally **not** fixed, reply with a polite explanation of why
   the team chose not to address it. If the thread was opened by `agtk code-review` and the
   user agreed the finding is not a defect, start the reply with a line
   `agtk: false positive: <reason>`. `agtk code-review approve` reads that line, and without
   it the finding keeps blocking approval.

   **A thread finding is answered on its thread**, against the root comment's `databaseId`
   from Phase 1:

   ```bash
   gh api -X POST 'repos/{owner}/{repo}/pulls/{number}/comments/{comment_id}/replies' \
     -f body="$REPLY"
   ```

   `gh` fills in `{owner}` and `{repo}` from the current repository. Replace `{number}` and
   `{comment_id}` yourself. `gh` has no subcommand that replies on a review thread, which is why the call above goes
   through `gh api`. **Never answer an inline comment with `gh pr comment`**, `gh pr review`,
   or a POST to `issues/{number}/comments`. The first and third post to the PR's conversation
   tab, and `gh pr review` opens a new review. In all three cases the reviewer's thread shows
   no answer and stays open. A **conversation** finding is the only one answered with
   `gh pr comment`, and that reply quotes the text it answers.

4. **Verify every reply landed** — check each thread reply against the response to its POST,
   and don't rely on having issued the command. The response's `in_reply_to_id` must equal
   the root comment's `databaseId`. Its `html_url` is the proof the reply exists.

   Then re-run the Phase 1 query. Every thread finding must show a comment by you
   (`gh api user --jq .login`) after the root comment. If any thread is missing one, post that
   reply now and check again. Only a missing reply sends you back here, so a reply is never
   posted twice.

   End with a table that has one row per finding: its number, where it was answered (thread
   or conversation), and the reply's `html_url`. A finding with no URL in that table has not
   been answered, and you must say so rather than report the job done.

5. **Request re-review** — request a new review from the reviewers who left comments,
   so they can verify the fixes. Use `gh pr edit` or the GitHub API to re-request reviews.

## Edge Cases

- **No unresolved comments** — if the PR has no open review comments, tell the user and stop.
- **Comments on deleted lines** — if a comment references code that no longer exists (e.g., from
  a previous revision), note this in the analysis and suggest marking it as resolved.
- **Conflicting reviewer opinions** — if two reviewers disagree, present both perspectives and
  let the user decide.
- **Large PRs** — if there are many comments (>15), consider grouping them by file or theme
  to make the analysis easier to digest.

## Tools

This skill relies on GitHub CLI (`gh`) and/or the GitHub MCP tools for:
- Fetching PR details and review comments
- Posting replies on review threads (`gh api .../comments/{comment_id}/replies`, never
  `gh pr comment`)
- Requesting re-reviews
- Pushing commits
