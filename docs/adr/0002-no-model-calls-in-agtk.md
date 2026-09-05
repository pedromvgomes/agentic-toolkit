# LLM-invoking work stays out of the agtk binary

`agtk`'s value is that `lock`, `fetch`, `render` and `sync` are reproducible. Putting a
model call inside the binary trades that away for auth, cost, rate limits and
non-determinism, and it would land on the path of commands that hooks and CI depend on
being deterministic.

Model-driven memory work is therefore never fired implicitly from a hook, and the
deterministic surface (`index`, `anchor`, `audit`, `lint`, `show`, `stats`, `candidates`) is
what it calls. The explorer ships as an agent definition, run by whatever agent the repo is
configured for. The curator does not: it ships in the binary, for the reasons in
`docs/adr/0004-the-curator-ships-in-the-binary.md`.

The rule is about the *path*, not the import graph. Unattended curation is a real need, so
one subcommand — `agtk memory curate` — does invoke an agent, through
`github.com/pedromvgomes/agentic-driver` (v0.1.0, `claudecode` and `codex` providers). The
library owns argv assembly, timeouts, exit codes, stderr redaction and isolated credential
construction; the provider is named in the `memory:` block rather than passed as a shell
command, because a raw string cannot express any of that and would duplicate, badly, the
provider abstraction the driver already has.

## Consequences
- The deterministic commands must stay reachable without a driver ever being constructed.
  That is the property hooks and CI depend on, and it is checkable by grep.
- Provider coverage is the driver's problem: a CLI it has no provider for is a gap to fill
  there, where dialect knowledge is tested, not an escape-hatch flag in agtk.
- The split is what lets Slice A be correct and fully tested before any model touches the
  store.
