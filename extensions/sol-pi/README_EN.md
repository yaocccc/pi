# SoL-Pi: ObservationPack + Online Context Compact

[中文](README.md) · [Installation](../README_EN.md)

## Copy just this capability and its UI

Copy these **two complete directories**, keeping the relative layout below. No `shared/`, whole repository, top-level `pi-ui/index.ts`, or build step is required. Thin support code is already included.

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/sol-pi" "$DEST/ui/sol-pi"
cp -R "$SRC/sol-pi/." "$DEST/sol-pi/"
cp -R "$SRC/pi-ui/sol-pi/." "$DEST/ui/sol-pi/"
~~~

~~~text
my-extensions/
├── sol-pi/index.ts        # capability, including all its local files
└── ui/sol-pi/index.ts     # optional UI, including all its local files
~~~

**Native Pi, explicit entry points:**

~~~sh
pi --no-extensions -e "$DEST/sol-pi/index.ts" -e "$DEST/ui/sol-pi/index.ts"
~~~

**Or register native UI yourself**, for example in `$DEST/my-ui.ts`:

~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/sol-pi/index.ts";
export default function (pi: ExtensionAPI) {
  registerUI(pi); // does not register/replace the capability
}
~~~

Load the capability and `my-ui.ts` instead of the UI entry above; do not load both registration paths or the whole-package adapter too. With automatic capability discovery, only add the UI registration. Without this UI, the capability still owns execution/validation; unsupported interactions must remain unavailable, never implicit consent.

For RPC, add `PI_UI_PROTOCOL=1` and `--mode rpc` to the explicit-entry command; the detailed wire contract follows below. Worker/Codex UI modules include their own optional method bridge and coordinate to register it once when combined. Other modules do not invent additional methods.

Pi 1.5.2 supplies the SDK, `pi-tui` and `typebox`; you do not need this repository's `node_modules` for these two directories. Use your own authentication, models and capability settings; copying source is not configuration. Keep directory names/layout as shown, or adjust imports yourself. Maintainers sync bundled helpers; consumers **do not** run generation scripts.

Local, auto-discovered Pi extension (`extensions/sol-pi/index.ts`). Adapted from [NVlabs/SoL-Pi](https://github.com/NVlabs/SoL-Pi), commit `1559b5cb12c72da4a485bc50fe326586b216fb19`; see [LICENSE](LICENSE) (MIT). Action Fusion has been removed; file changes and follow-up commands use native tools, sequenced through codemode when needed. OCC selectively ports upstream economics and plan/state fixes through commit `e1a586af0ad8956f42ae5b26bba20e48fbf30e00`, with a rewritten Pi lifecycle and local safety/display adaptations. **No Reducer or upstream global configuration loader is installed.** Validated on the globally installed **Pi 1.0.4**; no dependency install, settings change, or package upgrade is required.

- **ObservationPack:** pure-text, non-error tool results **larger than 10 KiB** are sent in full for the first **two provider requests**, then shown as a stable id/head/tail reference. Call `obs_recall` with `{"id":"obs_…","offset":0}` and follow `next_offset` to retrieve the exact archived text in pages. Short, error, mixed-media and `obs_recall` results are not packed.

Observations and an append-only JSONL ledger are archived in the Pi session directory under `sol-pi/<session-id>/observation-pack/{objects,ledger.jsonl}`. In-memory/`--no-session` runs instead use a private, random temporary root shared by the session in the current process. Temporary archives are retained after shutdown; their root mapping is not restored after a process restart. Session history is not rewritten; remove these archives when no longer needed (recall will then stop working). To uninstall, remove **only** `extensions/sol-pi/` and reload Pi; optionally remove the session archives separately. Do not load this entry again via an explicit extension path, or tools may be registered twice.

## Interface

SoL-Pi adds no promotional banners, savings messages, or background-tool cards. Both `obs_recall` and `update_plan` suppress their call/result renderers, including progress and error states; their model-visible results and background behavior are unchanged. Pi's native `edit`/`write` tools and display are unchanged. The post-compaction plan reminder remains hidden from the terminal.

## Online Context Compact (OCC)

`update_plan` replaces the complete working plan: `{"steps":[{"id":"build","goal":"Implement feature","status":"completed"},{"id":"test","goal":"Verify feature","status":"in_progress"}]}`. Statuses are `pending`, `in_progress`, and `completed`; optional `progress` contains arrays `files_changed`, `verification`, and `decisions`.

Only a **successful update_plan in the current turn that completes a previously registered unfinished step**, with work still unfinished, can trigger OCC. Newly introduced completed steps are historical records, not new boundaries. Compaction preserves the plan and prediction history; repeating or renumbering a completed plan does not create a fresh boundary. The economic gates can defer it; completing the whole plan never triggers it. At an eligible `turn_end`, OCC freezes a snapshot and starts a background summary through `ctx.modelRegistry.streamSimple()` with request-time authentication and native `compact()` prompts/retries. It does not wait for generation. A later `turn_end` / `agent_before_settle` atomically applies the summary with the latest plan state, preserving messages appended in the meantime. A hidden remaining-plan reminder and `continue: true` are added only while work remains. There is no main-session abort, awaited `ctx.compact`, or `sendMessage` restart.

Pi 1.0.2 nested `ctx.executeTool("update_plan", ...)` calls (including built-in codemode) can also authorize a boundary. OCC matches its own current-turn update ID to Pi's finalized `nestedCalls` record, not tool output text or `details`. The outer tool and every intermediate parent must succeed. Incomplete/truncated records, failed/blocked nested updates, and cancelled turns cannot authorize compaction; a plan update may still have been persisted before a parent failure.

A new external task after a completed plan resets task prediction history but retains accumulated cache debt and the last-compaction timestamp. Queued follow-ups are checked when delivered using bounded, run-local input matching. Pi 1.0.2 exposes no delivery source/input ID: mixed-source, duplicated or transformed inputs conservatively skip this reset. SDK injection of an identical bare user message bypassing `input` cannot be reliably distinguished.

### Settings and cost

- Requires a persistent `getSessionFile()`. No-session runs hide/deactivate `update_plan`; `PI_WORKER_DEPTH > 0` registers **no OCC tools or handlers**. ObservationPack remains independent.
- SDK hosts with an in-memory SettingsManager, a custom agent directory, or runtime overrides must inject `resolveSettings: ctx => ({ compaction: settingsManager.getCompactionSettings(ctx.model), retry: settingsManager.getRetrySettings() })` into `createOnlineContextCompactExtension`. The default resolver can observe persisted settings, not an inaccessible host's in-memory overrides.
- `cacheWriteReadRatio` defaults to **12.5**. This is a heuristic, **not measured provider pricing or a savings guarantee**. The factory accepts a nonnegative ratio or `null` (no economic-price estimate). A 1,000-token estimated memo, upstream request-horizon estimates, first-compaction multiplier, subsequent margin, and carried-debt gates drive the decision. Cache-write cost is estimated from the post-compaction context; savings use the actual native removable prefix, including split turns and replaced summaries, capped by the observed model-facing projection where available. Outstanding debt and repayment accumulate across compactions. A post-compaction cooldown guards economic re-compaction; window protection bypasses that cooldown and uses the same effective reserve-token setting. Decisions and committed debt consistently use the estimated memo size, not actual billing or measured summary size.
- Economic pricing anchors total context to native usage. For Responses APIs, an unchanged, uniquely matched **same-model** encrypted reasoning item can additionally earn removable-prefix credit from its assistant message’s reported `usage.reasoning` (once per message, subtracting already-counted visible thinking). Ciphertext length is never tokenized. Removed and retained reasoning share a budget bounded by current usage minus visible projected context; unmatched, unsigned, cross-model, missing/invalid-usage or usage-invalidated history earns no reasoning credit. Unattributed usage remains in retained cost. Historical output reasoning is only a replay-cost proxy, **not exact input billing or a guaranteed bound**. OP’s stateful hooks are not rerun, and native summary inputs are untouched.
- Each selected boundary spends **one or two additional model requests**, plus configured retries; split-turn history and prefix summaries are validated separately. Summary requests disable cache writes (`cacheRetention: "none"`). Compaction also invalidates the main prompt-cache prefix.
- OCC's persisted `requestCount` and request-horizon/debt-repayment estimates now count **main-agent `turn_start` events**, not provider transport attempts. Each started main turn (including the continuation after OCC) contributes once; retries within that turn do not contribute separately, and a turn aborted before dispatch may still count. Context growth is sampled at turn start. Summary requests and cache-warming replays never contribute. Existing saved counters remain usable but are not retroactively corrected.
- Successful checkpoint usage is stored on the compaction entry. Failed, cancelled, retried, or subsequently discarded summary work can still incur provider charges not fully represented by that successful entry. Economics is an estimate, not accounting or a budget cap.

### Compatibility and safety

Pi's preparation function is not publicly exported. `native-preparation.ts` originated as a **preparation-only port of Pi 0.87.1** and was **reviewed and adapted for Pi 1.0.2**, with differential tests rerun against **Pi 1.0.4**, with its [MIT license](extensions/online-context-compact/LICENSE.pi). It uses public projection APIs and the real active branch, preserving context edits, split turns, tool/result pairing, previous summaries, and cumulative file lists, including Pi 1.0.2 nested file calls. OCC retains the persisted `sol-pi-occ-native-0.87.1` format marker so existing `fromHook` checkpoints still carry file lists into subsequent OCC/manual compactions. Production uses only public package imports; private native comparison imports are test-only. Revalidate this port before moving to another Pi version; repository-local dependencies are **not a supported validation target**.

Boundary draft commits **do not fire `session_before_compact`, `session_compact`, or native compaction-failure hooks**. Extensions relying on those hooks to veto/observe all compactions will not intercept OCC; disable compaction/OCC if that is required. Manual/native compaction still uses normal hooks, and OCC reconciles it without double-counting. State is proposed in the same draft batch, never applied speculatively, and is reconstructed from the committed branch before subsequent operations. Earlier pure `custom` drafts are retained; earlier context-editing, custom-message, or compaction drafts cause OCC to skip. Later extensions can replace/drop the proposed batch and own any edits they make to it.

Aborted, deferred, empty, truncated, or tool-calling summaries never become checkpoints. Failures leave the existing scheduler alone. An independent controller cancels background work on corrections, aborted boundaries, session/tree/switch/fork/shutdown, model changes, or native compaction. Session identity, the complete captured branch prefix, generation, and main model are rechecked before returning drafts. Ordinary appended messages and metadata are preserved; branch changes, other compactions, branch summaries, or context edits invalidate the job. Late results cannot overwrite a newer job. Background validity checks test cancellation/generation before touching captured context; revoked getters after reload, session replacement, or SDK disposal permanently invalidate the job. Detached completion failures are contained without calling stale `ctx`/`pi` or writing diagnostics. OCC does not delete raw history. Summaries can still lose detail; inspect the original session or use `obs_recall` for archived observations when necessary.

OCC does not register a `before_provider_request` accounting hook: Pi 1.0.2 CacheWarmer reuses the main request's `onPayload` callback, including while the agent is streaming or awaiting a summary. Replays therefore append no OCC state, repay no OCC debt, and do not move the leaf through OCC. Successful native warming still appends its own `usage` entry and advances the raw session leaf; the summary validity guard permits those entries along with ordinary messages and metadata after the captured prefix. Conflicting context changes still invalidate the summary. User cache-warming settings and native usage accounting are unchanged.


### Pi 1.0.2 compatibility audit

Reviewed the installed `docs/extensions.md`, `docs/compaction.md`, session/SDK documentation, public declarations, and native `compaction/compaction.js` and `compaction/utils.js` rather than relying on a version-only test change:

| Area | Result / adaptation |
|---|---|
| Projected message extraction, empty histories and compaction leaves | Unchanged; system messages and previous compaction messages are not summarized as conversation. |
| Usage selection and projected token estimates | Unchanged; invalid/zero usage is ignored, later edits/compaction invalidate old usage, and only the current system prompt is estimated. |
| Cut points, split turns, tool pairing and recovery suffixes | Unchanged; omitted assistant recovery suffixes may advance the cut, but metadata alone, visible new input and external replacements cannot. |
| Repeated/retain-none compaction and previous summaries | Unchanged; selection follows the canonical active-branch projection. |
| File operations | Adapted to inspect `toolResult.nestedCalls.calls` as Pi 1.0.2 does. Available read/write/edit arguments are tracked even for failed/unfinished calls; omitted arguments cannot supply a path. This is attempted-file tracking, not proof of successful mutation. |
| `fromHook` file lists | Intentional local deviation retained: only our stable marked format supplements native file tracking, including old checkpoints. Unknown hooks remain excluded. |
| Public `compact()` and boundary drafts | Existing argument positions remain valid; native summary routing supplies its new optional session ID when omitted. Authentication, retries, cancellation, no-cache requests, atomic draft state and original-prompt continuation are tested. Nested plan authorization now follows finalized SDK records and successful ancestry. |

The upstream reference `e1a586af0ad8956f42ae5b26bba20e48fbf30e00` was **selectively ported, not copied wholesale**: post-compaction cache cost (`b20b7f7`), actual removable-prefix pricing (`efacbbf`), cumulative debt (`5c85108`), final plan-retention/progress semantics (`f997dde`) and ephemeral archives (`148b398`). Local projection-aware pricing and queued-follow-up handling supplement these fixes. Upstream abort/restart continuation and promotional UI remain excluded. The original SoL-Pi source commit above remains the port provenance. Local silent renderers, OP recall limits, main-turn-only accounting, warming isolation, and OCC worker/no-session exclusions remain in place; AF remains removed.

## Background compaction settings

`~/.pi/agent/online-compact-settings.json` (or under `PI_CODING_AGENT_DIR`):

```json
{
  "model": "auto",
  "thinking": "auto",
  "service_tier": "auto"
}
```

Model/thinking `auto` independently inherits the main session's setting at job start. Explicit models use `provider/model-id` (the model ID may contain `/`); thinking accepts `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, or `max`, subject to model support. Settings are reloaded and fixed for each job without changing the main session. Missing settings default to `auto`; invalid settings skip the optimization with a warning. SDK hosts can inject `resolveSummarySettings` for their own source.

`service_tier` accepts `auto`, `priority`, `fast`, or `ultrafast`: `auto` adds nothing; other values pass literally into the background summary payload without provider/model checks or conversion. This does not affect the main session or native `/compact`. Server support is required; unsupported parameters may fail the background summary.

No native compaction spinner or blocking wait is introduced; manual `/compact` and overflow recovery remain native. Notifications distinguish **summary ready** from **actually applied**, reporting generation time, token changes and total time including the safe-boundary wait. An idle result waits for a later boundary, never starting a conversation or writing the session itself. Obsolete/cancelled jobs are silent; actual failures report elapsed time and a short reason, not success.

Failure reasons use only exception messages, excluding stacks and serialized requests. Terminal controls are removed; common credentials and URLs are redacted on a best-effort basis, with a 300-character limit plus ellipsis. Raw errors are not saved. There are no OCC diagnostic logs or per-turn economic evaluations solely for logging; legitimate-boundary economic decisions and encrypted-reasoning accounting remain intact.

Token deltas compare the committed session projection before/after compaction with the same estimator, including intervening messages and the continuation reminder but excluding later messages. These are estimates, not measured/billed usage; out-of-session system prompts, tool definitions and subsequent OP transforms are excluded. They may differ from UI context counts. Growth is reported as growth, not savings.

## Offline validation

The historical runner pins Pi **1.0.4**. Its full .mjs suite has reproduced baseline failures on the current 1.5.x host; package `npm test` covers TypeScript tests only, not this suite. The 1.0.4 results below are historical, not a current-host pass claim.

The isolated harness also redirects `HOME` to keep tests separate from real home-directory state.

Run from the agent directory:

```sh
node extensions/sol-pi/tests/run-global.mjs
node extensions/sol-pi/tests/run-global.mjs --typecheck
```

The reusable runner requires global Pi **1.0.4**, copies only this extension into a temporary directory, and symlinks **existing** global dependencies there. It never changes installed `node_modules`, manifests, or lockfiles. `SOL_PI_GLOBAL_ROOT` can override `npm root -g`. Typechecking uses an existing global TypeScript compiler (directly installed or bundled with global ts-node), and never installs one.

The offline suite includes three native CacheWarmer regressions: idle/streaming replay isolation with carried debt, successful summary commits during warming with normal main continuation counts, and rejection of conflicting context edits hidden behind warming usage. Tests trigger the pinned Pi 1.0.4 CacheWarmer's real refresh directly (test-only private access), using the SDK callback chain and authenticated faux provider without waiting for timers. Other cases cover OP/OCC registration without native-tool overrides, observation packing and recall, real AgentSession + authenticated faux-provider runs with one/two compactions before the original prompt resolves, exact request/settlement counts, failure/cancellation/retry handling, final-plan and worker/no-session exclusion, default economic gates, state reopening/manual compaction, draft interference, native preparation parity, project trust, and lifecycle invalidation. No remote model calls are made. Tree/switch/fork/shutdown cancellation handlers also have focused unit coverage; this is not a live-provider billing or full interactive-UI certification.

Verified on global **Pi 1.0.4**: **174/174 tests pass in 10 files**, with no skips/cancellations, and strict TypeScript checking passes. Coverage includes non-blocking generation, idle-to-next-run application, new-message preservation, native manual/overflow preemption, model/thinking configuration, honest notifications with boundary-local token deltas and separate generation/total timings, native-preparation differential/legacy-marker checks, nested tool calls and successful ancestry, projected-prefix economics and cumulative debt, plan restatement/cooldown/task handoff, real queued follow-ups and cancellation, and persistent/ephemeral OP archives with byte-exact UTF-8 recall and fail-open storage errors. No remote model calls, real credentials, dependency installs, or global settings changes are needed.

Archived observations remain local and are not automatically deleted.

## Third-party UI quick start (self-contained)

### Load and observe; do not implement compaction in the UI
~~~sh
PI_UI_PROTOCOL=1 pi --mode rpc --no-extensions \
  -e /path/to/my-extensions/sol-pi/index.ts \
  -e /path/to/my-extensions/ui/sol-pi/index.ts
~~~
Use your own Pi credentials/settings. Keep all files inside the two copied directories; do not copy just index.ts. Node 22.19+ and the currently tested Pi 1.5.2 host are the baseline. PI_UI_PROTOCOL=1 opts into structured extension UI; the historical PI_GOUI_PROTOCOL=1 alias and pi-goui:* wire names remain compatible. No Go frontend is required. On older adapters that do not yet recognize PI_UI_PROTOCOL, replace that environment assignment with PI_GOUI_PROTOCOL=1; the wire payloads below are identical.

Online Context Compact requires its enabled configuration and a persistent primary session (not a Worker). The user owns model/auth settings. ObservationPack has separate archive behavior and may use temporary storage without a persistent session. UI integrations are **read-only observers**: there is no sol-pi.compact, plan.get/set or observation.read remote extension method. Standard Pi compact is a separate host operation, not an OCC control endpoint.

get_commands is a standard discovery request ({"id":"commands-1","type":"get_commands"}), but lists commands, not tools/services. Do not infer SoL-Pi presence from it, or send /update_plan or /obs_recall: these are tool names, not extension commands. Until a recognized plan/result/fact arrives, keep the ordinary Pi view and omit optional plan/progress widgets. Do not submit a model prompt merely to probe availability.

### Live and historical plans
On successful tool_execution_end for toolName="update_plan", read result.details (or details on a historical successful toolResult), for example:
~~~json
{
  "boundary": true,
  "completed_step_ids": [
    "inspect"
  ],
  "progress_recorded": true,
  "task_status": "active",
  "plan": [
    {
      "id": "inspect",
      "goal": "Inspect inputs",
      "status": "completed"
    },
    {
      "id": "apply",
      "goal": "Apply change",
      "status": "in_progress"
    }
  ]
}
~~~

plan is a full replacement, not a delta. Steps contain id, goal and status pending/in_progress/completed; preserve IDs and do not sort the supplied order. boundary/ completed_step_ids/ progress_recorded explain the recorded transition; task_status is currently active and is **not** a signal to close the agent session when every step is completed. Tool errors must not replace a committed plan with the proposed arguments.

For an initial or resumed view, send {"id":"history-1","type":"get_entries"}. A minimal illustrative response (real state entries contain additional fields) is:
~~~json
{
  "type": "response",
  "id": "history-1",
  "command": "get_entries",
  "success": true,
  "data": {
    "leafId": "entry-plan",
    "entries": [
      {
        "id": "entry-plan",
        "parentId": null,
        "type": "custom",
        "customType": "sol-pi-online-context-state-v1",
        "data": {
          "version": 1,
          "plan": [
            {
              "id": "inspect",
              "goal": "Inspect inputs",
              "status": "completed"
            },
            {
              "id": "apply",
              "goal": "Apply change",
              "status": "in_progress"
            }
          ]
        }
      }
    ]
  }
}
~~~

Follow parentId from leafId to identify the active branch; project the latest recognized version=1 custom state there, not the latest timestamp anywhere in the tree. Do not mutate or write these entries from the UI. Live entry_appended events can carry the same custom state; session replacement/fork/tree navigation requires rebuilding the view. Unknown versions and missing/invalid state leave a generic fallback, not a fabricated plan. Other state fields are internal and should be ignored by this UI projection.

### Progress facts and their exact transport
With pi-ui and the structured marker, standard notify messages carry facts:
~~~json
{
  "type": "extension_ui_request",
  "id": "notice-1",
  "method": "notify",
  "notifyType": "info",
  "message": "pi-goui:event:v1:{\"name\":\"context.applied\",\"data\":{\"beforeTokens\":12000,\"afterTokens\":7000,\"generationMs\":2400,\"durationMs\":8000}}"
}
~~~

After stripping the exact pi-goui:event:v1: prefix, parse {name,data}:
- context.ready: {generationMs}; a summary is ready, **not yet applied**.
- context.applied: {beforeTokens,afterTokens,generationMs,durationMs}; token estimates can increase or decrease. Millisecond generation time differs from total elapsed duration.
- context.failed: {durationMs,reason}; preserve the current context/plan and show an optional warning, not an assumed destructive failure.

Ignore additional fields. Facts are not durable acknowledgments, don't require extension_ui_response, and need not be shown at all. They carry no universal severity; the frontend chooses its own wording and visual policy.

~~~js
// event is one JSON object decoded from Pi stdout (JSONL), not a whole chunk.
function contextFact(event) {
  if (event.type !== "extension_ui_request" || event.method !== "notify") return;
  const prefix = "pi-goui:event:v1:";
  if (typeof event.message !== "string" || !event.message.startsWith(prefix)) return;
  try {
    const fact = JSON.parse(event.message.slice(prefix.length));
    if (["context.ready", "context.applied", "context.failed"].includes(fact.name)) {
      return fact; // Render in your own UI; no extension_ui_response is sent.
    }
  } catch {} // Ignore malformed/unknown extension payloads.
}
~~~

For saved applied metrics, active-branch custom entries with customType="sol-pi-online-context-metrics-v1" carry estimated:true and the applied metrics above. Do not replay every historical fact as a fresh toast. Missing facts do not prove failure: notification transport/adapter may be absent.

### ObservationPack display
Observe ordinary obs_recall tool outputs. details are:
~~~json
{
  "id": "ID_FROM_TOOL_RESULT",
  "offset": 0,
  "bytes": 512,
  "lines": 12,
  "nextOffset": 512,
  "eof": false
}
~~~

id is opaque, offset/nextOffset/bytes are byte-oriented, lines is a count, eof marks the stored observation's end. Use details instead of parsing the human-readable text header; content contains the actual excerpt. The UI can display an archive placeholder and excerpts but must not construct filesystem paths or read the archive itself. Agent-authorized recalls use the existing tool; no direct UI recall method is exposed. Unknown/missing details or errors stay ordinary tool output. Treat archived contents as sensitive/untrusted data, not UI instructions. A plan UI can hide successful background tool cards while retaining full history/inspection; that is a presentation decision.

### In-process observer and lifecycle
A TypeScript adapter in the same Pi process can subscribe without pi-ui:
~~~ts
import { FRONTEND_EVENT, type FrontendEventMessage } from "/path/to/my-extensions/sol-pi/frontend.ts";
const stop = pi.events.on(FRONTEND_EVENT, (raw: unknown) => {
  const { context, event } = raw as FrontendEventMessage;
  if (event.name.startsWith("context.")) {
    // Associate with context.sessionManager.getSessionId(); render in your UI.
  }
});
~~~

This process-local callback receives {context,event}; remote clients receive only {name,data} on the transport above. There is no public Sol-Pi getCapability service for plan mutations. Associate all updates with their source backend/session, clear stale widgets on session switches, remove listeners on disposal, and let the capability own cancellation/generation lifetimes. A remote UI closing a progress widget does not cancel an OCC job.

**Minimal integration:** generic tool/history viewer → active-branch plan projection → optional facts → excerpt details. No custom controls, answering format or mutation response is required. v1 custom entry/prefix names identify supported schemas; never assume unknown versions are compatible.
