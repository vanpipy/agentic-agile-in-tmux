# awp pi Extension

TypeScript file that awp passes to pi via `--extension <path>`
when spawning. Implements Phase 4 interception: whitelist /
blacklist matching + bridge to awp's TUI modal for unknown
commands.

## What is it?

A pi extension is a TypeScript module that hooks into pi's
event system. See `pi-mono/packages/coding-agent/src/core/extensions/types.ts`
for the full API.

awp uses the extension to:

- **Tool call interception**: on `tool_call`, check if the
  command matches a `block_patterns` entry (block), matches an
  `allow_patterns` entry (allow), or matches neither (ask the
  user via `ctx.ui.confirm`).
- **User bash interception**: same logic on `user_bash` for
  `!!`-prefixed commands.
- **Input / confirm forwarding**: on `input` / `confirm` events,
  forward to awp's TUI modal so the user can decide.
- **Config hot-reload**: on `session_start`, invalidate the
  config cache so user edits to `interception.json` take effect
  on the next tool call.

The extension communicates with awp's TUI via **pi's RPC stream**
(not a Unix socket — that approach was considered and rejected
during Phase 4 design; pi already forwards extension UI requests
to the client, so a separate socket is unnecessary).

## How is it loaded?

`PiClient.StartOptions` has an `Extensions []string` field. awp
passes `internal/pi/extension/awp-extension.ts` (absolute path) in
this slice. The path is resolved at spawn time.

## Exports

Pure helpers (testable without pi runtime):

- `matchRule(pattern, command)` — glob match.
- `isBlocked(cfg, cmd)` — block decision.
- `isAllowed(cfg, cmd)` — allow decision.
- `formatToolCallMessage(event)` — human-readable summary for
  the modal.
- `awpExtension(pi)` — default export; pi calls this at session
  start to register event handlers.

## Config

Default path: `~/.config/awp/interception.json`.

Override via env var `AWP_INTERCEPTION_CONFIG`. Schema:

```json
{
  "enabled": false,
  "block_patterns": ["rm -rf /*", "sudo *"],
  "allow_patterns": ["ls *", "cat *"],
  "blacklist": [],   // legacy alias for block_patterns
  "whitelist": []    // legacy alias for allow_patterns
}
```

`enabled: false` makes the extension a no-op even if other
fields are populated. User must explicitly opt in.

## TypeScript compilation

Plain TypeScript. pi uses Bun or ts-node to run it at spawn
time; awp does not compile it to JS itself.

Syntax verification (optional):

```sh
npx -p typescript tsc --noEmit --target es2020 --module esnext \
  --moduleResolution node --strict internal/pi/extension/awp-extension.ts
```

(pi's own type definitions must be available; install pi-mono or
use a stub `@earendil-works/pi-coding-agent` package.)