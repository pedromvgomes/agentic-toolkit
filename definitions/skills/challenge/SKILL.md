---
name: challenge
description: Stress-test a plan or design through relentless one-question-at-a-time interviewing until shared understanding is reached. MUST be used before presenting any plan for user approval (i.e. before calling ExitPlanMode in plan mode), and whenever the user says "challenge this", asks to stress-test a design, or wants to be challenged on a proposal. Walk down each branch of the decision tree, resolving dependencies between decisions one-by-one, with a recommended answer for each question. Use proactively whenever a plan has multiple ambiguous decisions, trade-offs, or unstated assumptions — do not wait for the user to ask.
---

# Plan Challenge

Interview the user relentlessly about every aspect of the plan or design until you reach shared understanding. The goal is to surface unstated assumptions, force trade-offs into the open, and leave nothing implicit before committing to a course of action.

## When this fires

- **Before plan approval (highest priority)**: Before calling `ExitPlanMode` to submit a plan for the user's approval, run this process first. A plan handed over without grilling is almost always under-specified.
- **On request**: User says "challenge me", "stress-test this", "challenge this", "challenge this design", or similar.
- **Proactively**: When a proposed approach has obvious branching decisions (e.g. "we could use X or Y"), trade-offs (latency vs cost, simplicity vs flexibility), or assumptions about the codebase, requirements, or constraints that haven't been explicitly confirmed.

## Process

1. Build a mental model of the decision tree: list every branch, assumption, and dependency in the plan.
2. Walk the tree top-down, resolving parents before children — a question's answer often eliminates whole subtrees.
3. For each open question:
   - State the question clearly.
   - Give your recommended answer with the reasoning behind it.
   - Wait for the user to confirm, override, or refine.
4. **Ask one question at a time.** Never batch. Batching collapses the dialogue and lets ambiguity hide.
5. **If a question can be answered by reading the codebase, read the codebase instead of asking.** The user's time is more expensive than yours.
6. Continue until every branch is resolved or the remaining branches are explicitly deferred.

## After challenging a Plan

Summarise the agreed plan with all decisions made explicit — including the ones that were originally implicit. Then proceed to `ExitPlanMode` or implementation.

## What to challenge on

- Scope boundaries (what's in, what's out, what's deferred)
- Failure modes and rollback strategy
- Interfaces with adjacent systems / teams
- Data shape, ownership, lifecycle
- Performance, cost, and operational expectations
- Migration / backwards compatibility
- Testing strategy and what "done" means
- Hidden coupling to existing code or conventions