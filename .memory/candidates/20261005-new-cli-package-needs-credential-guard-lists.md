---
about: "A new command that handles a token or key is covered by the credential guards only in internal/cli (no os.Setenv/os.Environ); os.WriteFile of a secret is banned only in the three credentialSurface packages"
saw:
  - source/toolkit/internal/cli/tests/credential_surface_test.go
  - source/toolkit/internal/cli/root.go
  - docs/adr/0019-a-token-reads-a-pull-request-and-never-posts.md
targets: credential-guards-are-hand-maintained-lists
verdict: still-true
---
Re-checked while scoping a SessionStart "cloud session init" command.

- `credentialSurface` (credential_surface_test.go:26) is still the three packages githubapp, reviewpost,
  reviewapprove; `environmentSurface` (:36) adds `internal/cli` and `internal/relay`.
- `TestTheCredentialIsNeverPutIntoTheProcessEnvironment` (:114) walks `environmentSurface`, so a command in
  `internal/cli` calling `os.Setenv`/`os.Environ` fails it (exporting GH_TOKEN that way is refused; a child
  process cannot export to its parent shell anyway).
- `TestNoInstallationTokenIsWrittenAnywhere` (:169) walks only `credentialSurface`, so `os.WriteFile` in
  `internal/cli` (e.g. writing an SSH key) is not caught; a new sibling package is not covered by anything
  until added by name. Test line numbers shifted from the note (`:78`->`:114`, `:133`->`:169`, `:190`->`:226`,
  `:230`->`:264`, `:275`->`:311`, `:351` token test unchanged in name).
