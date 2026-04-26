## Plan approval workflow

Before calling `ExitPlanMode` to submit any plan for approval, you MUST first invoke
the `challenge` skill and complete its interview process. The plan submitted to the
user via `ExitPlanMode` should be the post-challenging plan, with all decisions made
explicit. **This is non-negotiable** — a plan that hasn't been challenged is incomplete.