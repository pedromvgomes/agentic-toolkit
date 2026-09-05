# The curator ships in the binary, not as a definition

The curator's prompt lives in `internal/curator/`, embedded into `agtk`, and there is no
`memory-curator` agent definition. `agtk memory curate` names the roster and the tool grant
itself when it invokes the provider. This supersedes the sentence in
`docs/adr/0002-no-model-calls-in-agtk.md` that says model-driven memory work "ships as agent
definitions run by whatever agent the repo is configured for, never as subcommands" — for the
curator. The explorer is still a definition and still runs inside the host session.

The forcing constraint is `agentic-driver`'s `--setting-sources ""`, which is the first
argument of every `claude -p` it builds. `apiKeyHelper` is a settings-file key that outranks
an injected credential and is invisible to `env`, so refusing to load settings files is the
only thing that closes it. The same refusal puts a consumer's rendered `.claude/agents/` and
its pre-approved permissions out of reach: a headless run cannot see a definition that only
exists on disk.

## Considered options

**Read the rendered definition back.** `agtk` wrote `.claude/.agtk-manifest.json` and knows
where the curator went, so it could read that file and pass its body as the roster's prompt.
The curator would stay a definition, pinned by the lockfile like everything else. Rejected
because it makes `curate` unusable on a repo that has not rendered — which includes the repo
that just adopted memory, the case that most wants curation to work.

**Keep the definition and embed a copy.** Renders for in-session dispatch, embeds for headless
runs, one authored source. Rejected because `//go:embed` paths cannot climb out of their
package, so it needs a generated copy of the definition under a Go package plus a test to stop
the two drifting — machinery whose only purpose is to keep two homes agreeing about one thing.

## Consequences

- The curator's content policy ships with the binary, and the binary is installed separately
  from definitions, which are pinned by lockfile. A consumer on older definitions running a
  newer `agtk` gets the newer curator, and nothing says so. That is the price of the choice,
  and it is the one thing here worth watching.
- A platform `agtk` renders for is not a platform that gets a curator. Curation requires
  running `agtk`, whatever the host editor is. The explorer, which is still a definition, has
  no such restriction.
- The single-writer rule of `docs/adr/0003-the-curator-is-the-only-author-of-notes.md` splits
  in two. For the explorer it stays what that ADR says it is: instructed and guarded, not
  enforced. For the curator it becomes enforcement — the tool grant is constructed in Go at
  the call site as `--allowedTools`, an argv flag no settings file can widen, so the curator's
  own authority over `notes/` is bounded by code rather than by prose.
- `internal/curator` is the only package that constructs a driver. `index`, `anchor`, `audit`,
  `lint`, `show`, `stats` and `candidates` must stay reachable without one, which is the
  property hooks and CI depend on and is checkable by grep.
