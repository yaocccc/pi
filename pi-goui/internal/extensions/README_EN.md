# Optional extension integration guide

[简体中文](README.md)

This directory adapts third-party protocols, state and presentation; it does not execute backend business logic. See the [project README](../../README_EN.md) for usage and [AGENTS](../../AGENTS_EN.md) for general change constraints.

## Core defines contracts; extensions implement and export them

Authoritative interfaces live in [core/extension/contracts.go](../core/extension/contracts.go), with low-level projection/drawing contracts in [core/extensionapi/](../core/extensionapi/). The entry interface is:

```go
type Extension interface {
    Capabilities() Capabilities
}
```

Declare only supported capabilities; nil means absent. Confirm the backend protocol, implement the required interfaces, and join the `[]extension.Extension` returned by [extensions.New()](extensions.go). Existing examples are [hostui.Adapter](hostui/adapter.go), [projectionAdapter](projection.go) and [toolrender.Adapter](toolrender/adapter.go). Existing capabilities need no module-specific core branches; define new generic capabilities in core first.

| Capability entry | Responsibility |
| --- | --- |
| `NewSession` | Sync, async updates, sidebar, scrolling, polling, events and `ControlView` controls |
| `OpenDialog` | `DialogHandler` manages input, confirmation and cancellation through `DialogHost` callbacks |
| `NewInspector` / `NewInspectorContent` | `Inspector` manages details, interaction, child views and content versions |
| `NewUsage` | Observe authoritative records and return non-duplicated input/output usage |
| `NewProjection` / `Tools` | Session projection and optional card/full-detail providers |

[core/ui/extensions.go](../core/ui/extensions.go) is core's only implementation import point, composing a Runtime with `extension.New(extensions.New())`. Other core consumers use neutral interfaces, not the entire Model, concrete type aliases or mutable global registries.

## Dependencies and implementation ownership

- [hostui/](hostui/) owns third-party forms, controls, inspectors and private interaction state; it does not import `core/ui`.
- [toolrender/cards/](toolrender/cards/) and [toolrender/detail/](toolrender/detail/) own specialized cards/full details. Leaf packages reuse drawing through neutral callbacks without importing the parent renderer/UI. Only [toolrender/views.go](toolrender/views.go) assembles generic render providers; unrecognized tools return to generic fallback.
- [sessionview/](sessionview/), [solpi/](solpi/) and [web/](web/) handle live/history projection, metrics and visibility; [internal/protocol/](internal/protocol/) handles private wire recognition. Subpackages must not import the root assembly package.
- [askquestion/](askquestion/), [codexfast/](codexfast/) and [worker/](worker/) contain transport types, clients and read-only replay. Authoritative validation, permissions, scheduling, persistence, migration and private storage remain backend-owned; Go and TypeScript do not communicate through shared files.

## Composition and lifecycle

- Slice order selects the first tool, dialog, inspector, control or event handler. Observation, session sync and polling broadcast. Usage is summed; providers must not contribute the same record twice.
- Composite projections take the first nonempty plan, OR visibility predicates and use the first available metrics. Commit history snapshots only after successful validation; reject cross-instance snapshots. Worker replay creates its own projection on initialization/reset.
- Each Model owns a Runtime; factories create separate mutable state per tab, dialog and working run. ToolViews is an immutable per-runtime composition; nested inspectors and fallback views inherit the same provider.
- `Inspector.Version()` must change when private state changes detail rows; caches distinguish provider identity and version. Async results retain their original backend, page and revision.
- `Sessions.Control(ctx)` binds the selected owner. Requests after clicking go only to that owner, even if layout/visibility changes; only `Sessions.Poll` broadcasts.
- Empty slices, absent capabilities, unknown payloads and malformed data must not break native Pi, codemode or generic fallback. Protocol-looking user text must not become a command.

## RPC and fallback

Generic [Client.CallExtension](../core/rpc/extensions.go) discovers `name:"pi-ui-rpc", source:"extension"` via `get_commands` before sending `/pi-ui-rpc {"id":…,"method":…,"params":…}`. Results arrive as notify messages prefixed `pi-ui:rpc:v1:`, containing `{id,result}` or `{id,error:{code,message}}`; the outer prompt acknowledgement is not the operation result.

A missing bridge returns `ErrExtensionUnavailable`; never send an unknown slash prompt that could invoke a model. Correlate replies by request ID. Errors, cancellation, timeout and process exit clean up waiters and reject late responses. Local cancellation stops waiting; it does not roll back committed backend operations.

| Adapter | Required boundary |
| --- | --- |
| Questionnaire | Standard `extension_ui_request/response`, owned by dialog ID. Cancellation is `cancelled:true`, never invented answers or default consent; the backend validates answers authoritatively. |
| Codex levels | Only `codex-fast.get/set`; use backend `level/levels` and update after confirmation. Hide controls without the bridge/capability; do not read settings or infer levels from model names. |
| Worker | Only `worker.transcript`, with `descriptor:{version,id}` and optional opaque `cursor`; consume records and `reset/more/partial`. The backend validates paths, file identity, offsets, limits and cursor lifetime. |
| Worker presentation | Use stable `phaseCode`, not translated status text. Replay reconstructs presentation, never task execution. Activity is not a full transcript; missing end records are not success. Unavailable records fall back to existing results; preserve and label partial records. |

There are no public `worker.start`, `worker.cancel` or remote settings methods. `worker` is an Agent tool; `/worker_settings` is a dialog command. Do not invent RPC methods or fall back to reading private local logs.

Historical `pi-goui:*:v1` titles and the `pi-goui:event:v1:` prefix are compatibility wire identifiers, not Go-client requirements. Backends default to structured UI in RPC mode without protocol environment variables; do not rename these identifiers.

New adapters must cover normal behavior, instance isolation, absent bridge/capabilities, unknown protocols, cancellation and stale replies. Focused tests belong beside implementations; core retains genuine host integration tests. Verification commands live only in [AGENTS](../../AGENTS_EN.md#verification).
