---
description: Rules that govern how to choose, compare, and present solution approaches. They apply whenever proposing options, recommending a design, or deciding how to implement something.
---

**Implementation effort is not a decision criterion.** The time, number of steps, lines of code, number of files touched, or amount of work it takes *you* (or a developer) to build a solution must not influence which approach you recommend, how you rank options, or how you describe them.

Choose the approach that produces the best result for the system. Then build that.

## What still counts

These are properties of the *result*, not the effort to get there, and remain valid criteria:

- **Correctness** — handles the real problem, including edge cases and failure modes
- **Maintainability** — clarity, cohesion, coupling, ongoing cost to change and understand
- **Architectural fit** — consistency with existing patterns, boundaries, and conventions
- **Runtime characteristics** — performance, resource usage, scalability, latency
- **Reliability and operations** — observability, failure isolation, recoverability, operational burden
- **Security and compliance**
- **Testability**
- **Rollout risk** — blast radius in production, reversibility, data migration safety, backwards compatibility for consumers
- **Dependency cost** — new third-party dependencies, licensing, long-term support

Do not smuggle effort back in by relabelling it. "Simpler" must mean simpler *for the resulting system* (fewer moving parts, easier to reason about), not "less work to write". "Lower risk" must mean lower production or correctness risk, not "fewer changes for me to make".

## How to present options

- Rank and recommend purely on the criteria in "What still counts".
- Do not frame options as "quick fix vs. proper fix", "MVP vs. full solution", or "pragmatic vs. ideal" unless those labels reflect genuine differences in the result (e.g. a temporary mitigation needed because of a production incident).
- Do not include effort estimates, complexity-to-implement ratings, or phrases like "this would take significant work", "easier to implement", "less invasive", or "requires fewer changes" as arguments.
- Do not present a weaker option first or as the default because it's faster to build.
- If only one approach is actually good, say so and recommend it. Don't pad with inferior options just to offer a cheaper alternative.

## Exceptions

Effort may be considered only when:

1. **The user explicitly asks** for an effort estimate, a time-boxed solution, or a minimal change — then follow that request for that task.
2. **The user has set an explicit scope or constraint** (e.g. "only touch this module", "hotfix for prod, keep it small") — the constraint wins, and you note any resulting compromises in the result.

In either case, still state which approach would be best on the merits, so the trade-off is visible.

## Self-check before presenting a recommendation

Ask yourself:

- Would my recommendation change if implementation were instant and free? If yes, I'm weighing effort — revise.
- Does any argument in my comparison reduce to "less work"? If yes, remove it.
- Am I about to implement something narrower than what I recommended? If yes, implement what I recommended.