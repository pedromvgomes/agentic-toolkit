# code-panel-review toolkit

CLI invoked by the `code-panel-review` skill. Orchestrates four Copilot-SDK reviewer agents
and writes their JSON findings to a temp dir for Claude Code to synthesize.

## Build

Requires Node 24+ and Yarn 4. Deps are fetched from the Talkdesk Nexus registry
(`hub.talkdeskapp.com`) per `.yarnrc.yml`.

```bash
yarn install
yarn build   # esbuild bundles src/index.ts → dist/index.mjs with all deps inlined
```

`dist/index.mjs` is committed to the repo so the skill can run without an install step.

## Run (normally invoked by the skill, not directly)

```bash
node dist/index.mjs --repo "$(pwd)" \
  --include-committed-unpushed \
  [--include-staged] [--include-unstaged] [--include-untracked]
```

## Auth

The SDK reads credentials in this order: `COPILOT_GITHUB_TOKEN` → `GH_TOKEN` →
`GITHUB_TOKEN` → OS keychain (set by `copilot auth login`). A GitHub Copilot
subscription is required. BYOK is not supported in this toolkit.

## SDK note

`@github/copilot-sdk` is in public preview and the API is not frozen. Pin the version
in `package.json` and re-test after upgrades. The toolkit specifically exercises:
`CopilotClient()`, a `listModels`-compatible method on the client, `createSession` with
`model` / `systemMessage` / `excluded_tools`, and `session.sendAndWait`.
