---
description: |
  Before submitting any plan for user approval, the agent runs the `challenge` skill and
  submits the post-challenging plan, with all decisions made explicit.
---

## Plan approval workflow

Before calling `ExitPlanMode`, run the `challenge` skill on the plan and submit the plan
as it stands after that interview, with every decision it settled written out. A plan that
has not been challenged still carries decisions nobody stated, and approving it approves
those unseen.
