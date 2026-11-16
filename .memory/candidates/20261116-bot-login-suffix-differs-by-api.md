---
about: "GitHub names a bot account with a \"[bot]\" suffix over REST and without it over GraphQL, so code and skills comparing a bot's login either strip the suffix (backlog verify) or read every author by REST (pr-review-resolver --checked-authors)"
saw:
  - source/toolkit/internal/backlog/verify.go
  - definitions/skills/pr-review-resolver/SKILL.md
---

`appliedByTrustedApp` in `internal/backlog/verify.go` matches `strings.TrimSuffix(actor.Login,
"[bot]")` case-insensitively against `--trusted-app`, and only for `actor.Type == "Bot"`, because
the label event's actor arrives with or without the suffix depending on the API. The
`--checked-authors` rule in `pr-review-resolver`'s "Checked authors" section takes the other
route: it reads every author from REST listings (`pulls/<N>/comments`, `pulls/<N>/reviews`,
`issues/<N>/comments`) and never compares a GraphQL `author.login`, since the review App's login
it derives from `pulls/<N>/reviews` carries the suffix. A new comparison of a bot login has to
pick one of these two; comparing a GraphQL login to a REST one silently never matches.
