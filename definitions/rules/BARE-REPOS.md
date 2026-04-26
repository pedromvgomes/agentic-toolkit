---
name: bare-repos
description: |
  Personal/local repos use a bare-repo + worktree layout so multiple branches can be
  checked out in parallel without stash/switch dances. Apply this rule when working
  inside any repo that has a `.bare/` directory at its root.
type: rule
platforms: [claude, opencode, copilot]
---

# Bare-repo + worktree layout

Repos that follow this convention have this shape at their root:

```
<repo>/
  .bare/                  # the bare repo (do not run git commands from here)
  .git                    # file pointing at .bare
  main/                   # worktree for the main branch
  feature/<name>/         # worktrees for feature branches
  fix/<name>/             # worktrees for fix branches
```

## Rules

1. **Detect the layout** by checking for `.bare/` at the repo root. If present, this rule applies.
2. **Always operate from a worktree**, never from `.bare/`. The bare repo has no working tree — git commands run there will fail or behave unexpectedly.
3. **Place new branches in the matching folder by purpose:**
   - `feature/<branch-name>/` for new features and enhancements.
   - `fix/<branch-name>/` for bug fixes.
   - The folder name should match the branch name (or its last segment if the branch uses `/`).
4. **Create worktrees with `git worktree add`**, not `git checkout`:
   ```bash
   # from any existing worktree (e.g. main/)
   git worktree add ../feature/my-thing -b feature/my-thing
   ```
5. **Removing a worktree is a sequence, in this order:**
   ```bash
   git worktree remove ../feature/my-thing       # 1. unregister the worktree from .bare/
   git branch -D feature/my-thing                # 2. delete the local branch (-D since merge SHAs differ for squash/rebase merges)
   git push origin --delete feature/my-thing     # 3. delete the remote branch (may fail harmlessly — see below)
   git fetch --prune origin                      # 4. drop the stale remote-tracking ref
   rm -rf ../feature/my-thing                    # 5. remove the directory if anything remains
   ```
   - **Order matters:** never `rm -rf` first — that leaves a dangling worktree entry in `.bare/` that you'd then have to clean up with `git worktree prune`. And never leave the branch behind after removing the worktree; stale local branches accumulate fast.
   - **Step 3 often fails with `remote ref does not exist` — that's expected.** GitHub's "Automatically delete head branches" setting (on by default for new repos) deletes the remote branch the moment a PR is merged. The push-delete is a no-op in that case. Treat this exit code as success and continue to step 4.
   - **Step 4 (`fetch --prune`) is mandatory**, not optional. Without it, the local `remotes/origin/<branch>` tracking ref lingers and shows up in `git branch -a`, making the cleanup look incomplete.
6. **List worktrees with `git worktree list`** before creating a new one to avoid duplicates.

## Why

This layout lets the user keep multiple branches checked out simultaneously — useful when reviewing a PR while a feature is mid-flight, or when a hotfix interrupts other work. It also keeps the bare repo cleanly separated from any working state. Cleaning up branches alongside their worktrees keeps the repo's branch list aligned with what's actually active.
