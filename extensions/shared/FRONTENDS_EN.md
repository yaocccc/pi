# Capability and frontend boundaries

[中文](FRONTENDS.md) · [Installation](../README_EN.md)

**Copy-only distribution:** each capability and matching `pi-ui/<module>/` directory is self-contained as a pair; install them as `<target>/<module>/` and `<target>/ui/<module>/`. Consumers need neither this `shared/` directory nor the full UI assembly. Shared discovery/policy/transport helpers are maintenance templates with checked-in local copies; capability fact types and module-specific presentation live in their owners. `npm --prefix extensions run sync:portable` is a maintainer command, never an installation prerequisite.

## Ownership

Capabilities (worker, ask-question, codex-fast, sol-pi, etc.) own execution, authoritative validation, persistence, cancellation, lifecycle, history, and structured state. They must not import pi-ui/pi-tui, call ctx.ui, or register native renderers.

The optional pi-ui adapter owns native rendering, forms/drafts/confirmation, notification text, timers and legacy RPC UI compatibility. Dependency direction is **pi-ui → capability contracts**. Independent tool renderers never replace tool execution. Go renders its own UI; other frontends may provide their own adapters.

## In-process contracts

Each module's capabilities.ts (maintained from shared/capabilities.ts) discovers services through the session's event bus, not a module-global registry. Consumers look up a service when used, so loading order does not matter. These method objects are in-process interfaces, not serializable remote protocols.

| Name | Contract | Responsibilities |
| --- | --- | --- |
| worker:v1 | worker/api.ts | Detached session snapshots/history/subscriptions, settings/models/validated saves, bounded transcript reading |
| ask-question:v1 | ask-question/api.ts | ask, pending, queue, cancellation, answer validation |
| ask-question.interaction:v1 | ask-question/api.ts | Adapter supports/ask methods, cancellation, untrusted submissions |
| codex-fast | codex-fast/api.ts | Supported levels, reads, authoritative saves; codex-fast:changed facts |

Without an interaction port, questions return unavailable and unanswered questions, never assumed consent or fabricated cancellation. The questionnaire capability owns FIFO and shutdown; the adapter owns surfaces/editor access. Worker refresh timers belong to pi-ui and are released after the capability session closes. Commands /worker_settings and /codex-fast belong to pi-ui, not capabilities.

## Facts and remote compatibility

Each capability's own frontend.ts emits pi:frontend-event:v1 with {context,event} and owns its event types. shared/frontend.ts and frontend-events.ts are whole-package compatibility/type aggregation only. It does not identify frontends or generate notification text. Observers must not change business results. Saved facts originate from successful capability commits; UI decides presentation.

Legacy Go bridges remain in pi-ui:
- Any RPC frontend may opt into structured UI with `PI_UI_PROTOCOL=1`. The legacy `PI_GOUI_PROTOCOL=1` and questionnaire-only `PI_GOUI_ASK_QUESTION=1` remain supported. Flags are read only by adapters; the preserved `pi-goui:*:v1` wire names do not require a Go client.
- extension_ui_request titles pi-goui:ask-question:v1, pi-goui:worker-settings:v1 and pi-goui:codex-fast:v1, with JSON placeholders.
- Fact notifications prefixed pi-goui:event:v1:.

The optional **pi-ui-rpc** extension command accepts {id,method,params} via ordinary Pi RPC. Correlated replies are notify messages prefixed pi-ui:rpc:v1:. Go first checks get_commands for a command whose source is extension; an unknown command must never accidentally reach the model.

Current methods:
- codex-fast.get / codex-fast.set delegate reads and writes to the capability.
- worker.transcript delegates to readTranscript; the capability validates descriptors, paths and cursors and bounds reads.

The bridge routes and validates transport structure only. It does not own file access or business validation. Missing methods/capabilities return unavailable; frontends hide or degrade optional features rather than access private files.

All third-party integration implementations live in `pi-goui/internal/extensions`: capability subpackages contain protocols/replay, `hostui` contains forms/controls/inspectors, and `toolrender` contains cards/full detail views. Core UI and renderers retain only generic components and thin delegates. Codex never reads/writes its capability configuration in Go. Worker private recordings are accessed only through `worker.transcript`, and slot notices use stable `phaseCode` values rather than Chinese text. Generic session browsing, Git/quota display, editors and other UI helpers remain frontend features.

RPC reply limits account for the second JSON encoding of `notify.message` (32 MiB budget, below the 64 MiB receiver line limit). Oversized results return `response-too-large` instead of disconnecting the backend. Automatic naming and Telegram use explicit primary/Worker session policy, not `hasUI`.

## Source layout and regression coverage

- shared/: maintenance templates for discovery/policy/token helpers and compatibility event aggregation; no extension entry or copy-install dependency.
- pi-ui/{ask-question,worker,codex-fast}/: UI/transport adapters.
- pi-ui/<module>/: module-owned notification text, native UI and RPC adaptation; pi-ui/shared/ retains thin helper templates and whole-package compatibility facades.
- pi-ui/sol-pi.ts: hidden cards for obs_recall/update_plan, without suppressing tool results.

Go consumes structured worker-ui data, not ANSI/components. Telegram is an external integration, not native terminal rendering. Session names are shared metadata.

Changes should run architecture tests, actual SDK loader tests and affected capability/adapter tests. Cover native TUI, Go RPC, generic RPC and absent adapters. Automated coverage does not certify all manual interactions or host versions.
