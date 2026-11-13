# Working on Pi Go UI

[中文](AGENTS.md)

These instructions apply to this directory. Also follow the repository's parent `AGENTS.md`. Inspect the working tree first, preserve unrelated edits, and do not commit unless asked.

## What this project is

Pi Go UI is a standalone Go/Bubble Tea frontend for **standard Pi RPC**. It launches/connects to a Pi backend; it does not embed the TypeScript extensions or require this repository's checkout layout, models, credentials, or personal settings. The extensions shipped beside this project are optional third-party integrations, not core backend features.

Read `README_EN.md` for the project architecture and implementation boundaries. Do not add a separate DESIGN document; AI modification guidance belongs to AGENTS. Default documents are Chinese, with `_EN.md` English counterparts linked at the top. For extension work, start with `internal/extensions/README_EN.md`, `internal/extensions/hostui/README_EN.md`, and `internal/extensions/toolrender/README_EN.md`.

## Where code belongs

Paths below are relative to `pi-goui/`.

| Location | Responsibility |
| --- | --- |
| `cmd/pi-goui/` | Application entry and CLI |
| `internal/rpc/` | Standard Pi transport, request correlation, lifecycle, generic extension-RPC envelope |
| `internal/session/` | Generic session/message projection; optional projections attach through thin integration files |
| `internal/ui/` | Host Model, event loop, generic layout/input, tab/inspector ownership and thin adapter bindings |
| `internal/render/` | Generic rendering, wrapping, sanitization, frames and complete-value formatting |
| `internal/extensionapi/` | Small neutral rendering contracts; no third-party behavior |
| `internal/extensions/<name>/` | An extension's wire types, typed RPC client, pure decoding and read-only projection |
| `internal/extensions/hostui/` | **Actual** extension forms, controls, inspectors, interaction and presentation state |
| `internal/extensions/toolrender/cards/` | **Actual** extension-specific compact cards |
| `internal/extensions/toolrender/detail/` | **Actual** extension-specific full tool-result views |
| `internal/extensions/dialogs.go`, `notices.go` | Optional protocol recognition and fact-notification presentation |

Do not scatter new third-party implementations in core UI/render packages and merely name them `extension_*.go`. Those existing core files are thin bindings: selecting the originating tab/inspector, passing neutral inputs, scheduling `tea.Cmd`, closing/focusing a dialog, and rejecting stale results. Implement extension behavior under `internal/extensions/`.

### Dependency direction

- `hostui` must not import `internal/ui` or receive/duplicate the complete application Model. Pass small state views, callbacks or owner handles. It may reuse generic `internal/render` APIs.
- `toolrender/cards` and `toolrender/detail` must not import parent `internal/render`, `internal/render/tooldetail`, or `internal/ui`. Use `extensionapi` callbacks and generic `render/frame`, `render/tooldata`, `config`, and `session` as needed.
- The core renderer's `extensions.go` facades bind generic callbacks and route requests; they are not alternative implementations.
- Host callbacks are instance-local. Do not introduce mutable global rendering registries, share view state across backends, or expose host internals simply to move a file.
- Generic session projection must not depend on Worker replay; the Worker adapter may consume generic session projection.

## Adding a third-party integration

1. **Establish the backend contract first.** Read that extension's own README: discovery, request/result schemas, version markers, lifecycle, cancellation and unavailable behavior. Do not infer methods from a tool name. For this repository's capabilities, each `../extensions/<name>/README_EN.md` is a standalone integration guide.
2. **Choose the narrowest surface.** Prefer ordinary Pi tool results, dialogs and notifications when sufficient. Add a typed adapter under `internal/extensions/<name>/` only for the extension-specific protocol/projection. Keep business rules in the backend.
3. **Add presentation in the dedicated directories.** Add forms/controls to `hostui`, compact cards to `toolrender/cards`, and full detail views to `toolrender/detail`. Reuse generic wrapping, sanitization and full-value formatting rather than copying the renderer.
4. **Wire thin host routes.** For a dialog, extend the recognizer in `internal/extensions/dialogs.go` and the `hostui` dispatch, then use the existing small `DialogHost` bridge. For a full-result renderer, extend `detail.Render`; unknown tools must still return false and use generic rendering. For a compact card, route through the renderer facade and return generic hit blocks. Add only the necessary Model event/lifecycle bindings.
5. **Keep integrations optional.** Absence, unsupported versions, malformed/unknown payloads and disconnects must not break ordinary Pi interaction. Hide unsupported controls or show the existing result/standard dialog; do not reinterpret protocol-looking user text as commands.
6. **Test and document the contract and fallback.** Include the no-extension path and lifecycle/ownership behavior, not only the successful specialized view. Update the relevant adapter README and any changed backend guide.

### Backend authority is not frontend authority

Execution, permissions, scheduling, authoritative validation, persistence, migration, transcript authorization and private storage belong to the extension backend. Go must not read/write third-party config files, guess private log paths, reconstruct missing settings from model names, or silently substitute a local implementation.

This is **not** a ban on all frontend local I/O. Generic session browsing, clipboard/editor integration, images, Git, quota display, updates and frontend preferences remain frontend responsibilities.

For the TypeScript modules in this repository, copy-only consumers need `<module>/` plus `ui/<module>/` (copied from `extensions/pi-ui/<module>/`), and may register the native UI explicitly. This is a backend installation option, **not** a Go source/runtime dependency. Never add Go code that searches for those directories or imports their files.

## RPC and lifecycle rules

- Use `Client.CallExtension(ctx, method, params)` for the optional method bridge. It first discovers `pi-ui-rpc` through `get_commands` with `source == "extension"`. Never send an undiscovered slash command: it could start a model turn.
- Bridge operation replies use a notify message prefixed `pi-ui:rpc:v1:` and an inner `{id,result}` or `{id,error:{code,message}}`. The outer Pi prompt response is not the operation result. Keep ordinary notifications intact.
- Structured dialogs use standard `extension_ui_request` / `extension_ui_response`; correlate the dialog ID. Dismissal is `cancelled:true`, not a fabricated answer or implicit consent.
- Historical `pi-goui:*:v1` titles and fact prefix `pi-goui:event:v1:` are compatibility wire identifiers, not proof that the client must be Go. Backend adapters also accept the neutral `PI_UI_PROTOCOL=1` opt-in. Do not rename wire constants casually.
- Currently implemented method names are `codex-fast.get`, `codex-fast.set`, and `worker.transcript`. Worker transcript parameters use **`descriptor`**, plus an optional opaque backend cursor. There is no published `worker.start`, `worker.cancel`, or remote Worker-settings API. `/worker_settings` is a dialog command; `worker` is an Agent tool.
- Local timeout/cancellation stops waiting; it does not undo a committed backend write. Ignore late replies, dispose timers/listeners on close, and clear pending waits on backend exit.
- Capture the originating backend/tab/inspector and revision for async work. Never apply a late reply to whichever tab happens to be active. Do not mutate UI state from goroutines; return messages/commands through the event loop.
- Unknown/missing recording data falls back to the saved task result; preserve partial records and label incompleteness. A missing end record is not success. Use stable codes such as `phaseCode`, not translated labels, for logic.
- Render backend text as untrusted content through generic safety helpers. Keep protocol IDs/private payloads out of user-facing summaries unless intentionally needed. Do not follow paths or execute text found in a result.

## Verification

Run from `pi-goui/`; prefer focused checks while iterating:

```sh
go test ./internal/extensions/... ./internal/render/... ./internal/ui
```

Before delivering a cross-cutting change:

```sh
go test ./...
go vet ./...
GORACE=atexit_sleep_ms=0 go test -race ./... -timeout=90s
git diff --check
```

Use `gofmt` on changed Go files. Existing boundary tests enforce adapter dependency direction. Keep host integration tests for output, ANSI safety, width, folding, animation and routing, and put adapter-local behavior tests beside the implementation. For async features cover stale owners, tab/session switches, cancellation and disconnects. Test unavailable capability/bridge and unknown protocol fallback explicitly.

For packaging/portability changes, build the binary and run `--check --no-session` from an unrelated temporary project with an isolated HOME and `PI_CODING_AGENT_DIR`, without copying the author's settings/models/auth. This verifies transport startup, not real model inference or terminal interaction.

If modifying the companion TypeScript modules, also run `npm --prefix ../extensions run typecheck` and `npm --prefix ../extensions test`. Portable support is checked in; maintainers synchronize its templates with `npm --prefix ../extensions run sync:portable`. Consumers must not need that generation step.

Report exactly what was verified. Do not claim real-terminal, cross-platform or all-repository success from unit tests alone. Known historical PTY/SoL-Pi runner compatibility failures are separate from this Go suite; investigate attribution rather than weakening tests or broadening an unrelated change.
