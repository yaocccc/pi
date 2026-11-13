# Optional third-party UI adapters

[中文](README.md)

Pi Go UI is a standalone Pi RPC client. This repository's extensions are **not core frontend dependencies**. They are optional integrations, like extensions supplied by another user.

## Code ownership

| Location | Responsibility |
| --- | --- |
| `internal/rpc/` | Standard Pi transport and the optional, capability-discovered extension RPC envelope; no Worker/Codex operations |
| `internal/extensions/` | Third-party protocol recognition and notification formatting; unknown requests retain standard Pi behavior |
| `askquestion/` | Questionnaire wire types; no authoritative answer validation |
| `codexfast/` | Typed `codex-fast.get/set` RPC client; no configuration files or migration |
| `worker/` | Worker settings wire types, transcript RPC, and read-only replay; no private log filesystem access |
| `solpi/` | Read-only plan projection; no compaction or plan mutation |
| `toolrender/cards/` | Worker/questionnaire compact cards, task activities and Codex mode labels |
| `toolrender/detail/` | Worker/questionnaire/SoL-Pi/web tool full detail views; unknown fields preserved |
| `internal/extensionapi/render.go` | Neutral rendering types and host callbacks, no third-party behavior |
| `internal/render/extensions.go`, `internal/render/tooldetail/extensions.go` | Thin host callback binding, aliases and dispatch only |
| `hostui/` | Third-party controls, forms and inspectors using neutral UI host callbacks |
| `internal/session/extension_plan.go` | Connects optional plan data to the generic sidebar state |

Tool rendering implementations live only under `toolrender/`, not in prefixed files in the host renderer. Adapters receive generic wrapping, sanitization, frame and full-value formatting callbacks; they never import the host `render` or `ui` implementations. Shared JSON decoding lives in `render/tooldata`; it has no extension rules. Unknown tool kinds retain the generic host result view. Host integration tests remain in `render`, while adapter-local tests cover host isolation and protocol projection.

## Optional RPC bridge

`extensions/pi-ui` provides `/pi-ui-rpc` in a Pi backend. The Go client first checks `get_commands` and requires an **extension** command with that name. Without it, it returns `ErrExtensionUnavailable` and does not send a prompt that might start an LLM turn.

Methods are registered by adapters and delegated to capability services. The current clients use `codex-fast.get`, `codex-fast.set`, and `worker.transcript`. Replies are correlated by request ID; errors, cancellation, and process shutdown clean up local waiters. Client cancellation does not roll back a backend operation already committed.

No bridge or capability means no Codex tier control, and Worker inspection falls back to the task result already received. It never probes local extension files. Go and TypeScript communicate only through the Pi RPC process, not imports, shared files, or assumptions about a repository path.

## What remains in the frontend

General-purpose UI facilities—session browsing, clipboard/editor integration, image previews, Git display, quota display, update notices, and frontend preferences—remain frontend features. They do not require these third-party extensions. This boundary does not claim that all local I/O has been removed from the entire application.

To add an integration, put its wire contract/client/projection here, keep business validation and persistence in its backend, and add absent-extension and generic-fallback tests. No third-party package is loaded directly by Go.
