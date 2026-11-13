# Pi UI Adapter and Capability Boundaries

[中文](README.md) · [Installation and module index](../README_EN.md)

Optional adapter for native tool rendering, questionnaires, settings menus, notifications, refresh, and structured RPC. Load index.ts for the whole package; for standalone copies, use pi-ui/<module>/ as documented by the capability README. Do not assume copying the whole pi-ui directory alone is sufficient: its aggregate entry still imports sibling capabilities/shared.

## Ownership and in-process interfaces

- Capabilities own execution, authoritative validation, configuration persistence, cancellation, request lifecycles, history, and structured state. They must not import pi-ui/pi-tui, call ctx.ui, or register renderers.
- UI consumes interfaces and adds presentation with `pi.registerToolRenderer`, without replacing tools or execute. Go/Web frontends render independently; native components are not directly reusable.
- Each module's capabilities.ts uses `registerCapability/getCapability` over the host event bus `pi:capability:discover:v1`, with no module-global registry. Discovery is on demand and load-order independent. These are in-process methods, **not remote RPC**.
- Capability-owned frontend.ts files publish `pi:frontend-event:v1` facts as `{context,event}`, without checking frontend identity. Facts are detached, deeply read-only snapshots with an immutable ownership envelope; the live host context retains its identity and is not frozen. Observer failures must not affect business outcomes. UI owns wording/notifications; successful-save facts follow backend persistence.
- shared and pi-ui/shared provide maintenance templates/compatibility aggregation for discovery, transport, session policy, and token helpers; copies ship with modules. Primary/Worker execution policy is explicit, not inferred from hasUI.

| Capability | Interface and ownership |
| --- | --- |
| `ask-question:v1` | [api.ts](../ask-question/api.ts): ask/pending; capability owns FIFO, cancellation, validation |
| `ask-question.interaction:v1` | Same API: UI implements supports/ask, receives questions/cancellation, returns untrusted answers |
| `worker:v1` | [api.ts](../worker/api.ts): session snapshots/history/subscriptions, settings/models/save, bounded recording reads |
| `codex-fast` | [api.ts](../codex-fast/api.ts): supported tiers/get/save; codex-fast:changed reports successful persistence |

Without an interaction adapter, questionnaires return unavailable and unanswered questions—not fabricated cancellation or consent. Questionnaire UI owns editor occupancy/closing; Worker supplies detached snapshots while UI owns refresh timers. On session close/replacement, unsubscribe and clear timers/pending requests; never deliver an old owner's result to a new page.

`/worker_settings` and `/codex-fast` are UI commands, not capability entries. Other hosts may consume in-process services without imitating menus. Authoritative validation and private storage must remain in the backend. Worker settings shape validation, error types, and recording DTOs belong to api.ts. UI may precheck shape, but saveSettings still authoritatively verifies models/thinking and persists; UI must not import config.ts/settings.ts storage implementations.

## Third-party UI: discovery and transport

With the relevant adapter loaded, ctx.mode=rpc enables structured UI by default. PI_UI_PROTOCOL / PI_GOUI_PROTOCOL are no longer switches and need not be set. Only questionnaires retain the legacy non-TUI PI_GOUI_ASK_QUESTION=1 path. Historical `pi-goui:*:v1` names do not require a Go client.

1. Call get_commands on the relevant Pi backend. Use pi-ui-rpc, worker_settings, or codex-fast only if discovered with source=extension. Tools/capabilities are not listed; absence here does not prove they are missing.
2. Unknown slash commands may become model input: **never send one as a probe**. Without the bridge, disable specialized controls while preserving native Pi/codemode/generic tool views.
3. Correlate IDs per backend, session, and request. Ignore late responses and clear waiters on page close, backend replacement, cancellation, or timeout. The bridge does not set client timeouts.

### Generic bridge

Only these methods are exposed. Capabilities validate business parameters; the bridge only routes and validates transport:

| method | params |
| --- | --- |
| codex-fast.get | {} |
| codex-fast.set | {level:"normal"\|"fast"\|"ultrafast"} |
| worker.transcript | {descriptor:{version:1,id:UUID},cursor?} |

There are no worker.start/cancel/settings.get/settings.save, plan.get/set, sol-pi.compact, or observation.read remote methods. Pi abort is not a control API for a specific Worker batch.

Complete read-only request (the UUID is illustrative; use the actual task descriptor):
```json
{"id":"rpc-1","type":"prompt","message":"/pi-ui-rpc {\"id\":\"read-1\",\"method\":\"worker.transcript\",\"params\":{\"descriptor\":{\"version\":1,\"id\":\"11111111-1111-4111-8111-111111111111\"}}}"}
```

The ordinary response only acknowledges the outer rpc-1 command. The result arrives in a separate notification:
```json
{"type":"extension_ui_request","id":"notice-1","method":"notify","notifyType":"info","message":"pi-ui:rpc:v1:{\"id\":\"read-1\",\"result\":{\"cursor\":\"22222222-2222-4222-8222-222222222222\",\"reset\":true,\"records\":[],\"more\":false,\"partial\":false}}"}
```

Strip only the exact `pi-ui:rpc:v1:` prefix and correlate the inner read-1, not the notification id. Do not send extension_ui_response. Success is {id,result}; failure is {id,error:{code,message}}, for example:
```json
{"id":"read-1","error":{"code":"unavailable","message":"Extension capability unavailable"}}
```

Transport errors include invalid-request/unavailable/extension-error/response-too-large; capability guides own business errors. The UTF-8 size of **JSON-encoded notify.message** is limited to 32 MiB. Oversized replies produce a short error, not a backend disconnect. Unknown messages, malformed payloads, and unmatched IDs must not update state.

Read recordings only through worker.transcript: the backend validates descriptors/paths/cursors and bounds reads. Frontends must not guess paths, fabricate cursors, or read extension-private files. See [Worker](../worker/README_EN.md) for pagination and missing-record handling.

### Dialogs and fact notifications

| wire | Handling |
| --- | --- |
| input/title=pi-goui:ask-question:v1 | Questionnaire JSON in placeholder; reply with a JSON-string value or cancelled:true; see [questionnaires](../ask-question/README_EN.md) |
| input/title=pi-goui:worker-settings:v1 | placeholder contains config/original/models/error; see [Worker settings](../worker/README_EN.md#settings-transaction) |
| input/title=pi-goui:codex-fast:v1 | Return a tier string; see [Codex Fast](../codex-fast/README_EN.md) |
| notify/message=pi-goui:event:v1:… | Parse {name,data} facts without replying; each capability guide defines its names/data |

The backend serializes, cancels, and validates questionnaires. Preserve request IDs, originating backend/tab ownership, and cancellation semantics; do not route replies by current focus. Unavailable interaction is not authorization, and closing a view must not bypass the backend lifecycle. Ignore extra notification fields and degrade on unknown/malformed events. Do not replay historical facts as fresh notices; missing notices do not prove a save failed.

## Extension work and verification

Go implementations live in [pi-goui/internal/extensions](../../pi-goui/internal/extensions/README_EN.md), assembled per instance through core-owned interfaces. They do not read/write Codex configuration or Worker-private files. Other frontends must preserve the same backend ownership and safe fallback boundaries.

Define a new capability's own interfaces/facts before adding its UI adapter; execution, authorization, and persistence stay in the backend. See the [extension overview](../README_EN.md#update-uninstall-and-maintain) for development/test commands. Boundary changes require checking native TUI, Go RPC, ordinary RPC, and no-UI paths—not just rendering snapshots.
