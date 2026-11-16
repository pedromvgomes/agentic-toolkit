---
about: "ReadIssueTrust pages userContentEdits and the label timeline with separate queries, and refuses the read if any later page reports a different body, lastEditedAt or either totalCount, or if the collected nodes do not add up to totalCount; the itemTypes-filtered timeline totalCount counting only the filtered types is assumed, not checked live"
saw:
  - source/toolkit/internal/githubapp/issue_trust.go
  - source/toolkit/internal/githubapp/tests/issue_trust_test.go
---

`(*Client).ReadIssueTrust` in `githubapp/issue_trust.go` takes body, author and the first 100 of
both connections in one query, then pages each connection by its own cursor. `sameIssueState`
compares every follow-up page against the first answer, so the returned body is always the one
whose history was read. After paging, `len(edits)`/`len(events)` must equal the first page's
`totalCount`. If GitHub's `timelineItems(itemTypes:[LABELED_EVENT,UNLABELED_EVENT]).totalCount`
counted unfiltered items, every issue with other timeline events would be refused (fail closed).
The claude.ai cloud proxy refuses GraphQL, so this was never run against the live API from there;
that refusal surfaces as `ErrGraphQLUnavailable`. `NOT_FOUND` is detected by the
`"TYPE: message"` format of `GraphQLError.Messages` in `graphql.go`.
