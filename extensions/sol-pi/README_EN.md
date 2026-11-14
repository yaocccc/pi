# SoL-Pi

[中文](README.md) · [Installation overview](../README_EN.md)

ObservationPack archives/recalls large tool results; Online Context Compact (OCC) summarizes context in the background at plan-safe boundaries. Native file tools are unchanged; Action Fusion, Reducer and the upstream global configuration loader are not installed. Adapted from [NVlabs/SoL-Pi](https://github.com/NVlabs/SoL-Pi) (original commit `1559b5cb`, selective ports through `e1a586af`); see the [MIT license](LICENSE).

## Install: two complete directories

No shared directory, whole repository, top-level UI entry or generation step is required. Preserve the layout; do not copy index.ts alone:
~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/sol-pi" "$DEST/ui/sol-pi"
cp -R "$SRC/sol-pi/." "$DEST/sol-pi/"
cp -R "$SRC/pi-ui/sol-pi/." "$DEST/ui/sol-pi/"
pi --no-extensions -e "$DEST/sol-pi/index.ts" -e "$DEST/ui/sol-pi/index.ts"
~~~

Alternatively, register UI explicitly in `$DEST/my-ui.ts`:
~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/sol-pi/index.ts";
export default function (pi: ExtensionAPI) {
  registerUI(pi);
}
~~~

Then load the capability entry and my-ui.ts, **not also the UI entry or whole-package adapter**. Do not explicitly reload an auto-discovered capability either. UI does not register/replace capabilities; execution and validation remain capability-owned without it. Unsupported interactions never imply consent.

Add `--mode rpc` for RPC; structured UI is the default, without protocol environment variables. The installation baseline is Node 22.19+ / Pi 1.5.2, which supplies SDK, pi-tui and typebox; repository node_modules is unnecessary. Supply your own models, authentication and settings. **Installability does not establish OCC compatibility with that SDK**; see below.

## Tools and data

- `update_plan` replaces the complete `steps` array; preserve stable IDs and order. Status is `pending|in_progress|completed`, with at most one in_progress. Optional `progress` contains three string arrays: `files_changed/verification/decisions`. Register unfinished steps before completing them; newly introduced completed steps are historical. See the full [schema](extensions/online-context-compact/tools.ts).
- ObservationPack packs only **pure-text, non-error results larger than 10 KiB**. The first **two provider requests** retain full text; later requests receive a stable ID/head/tail/recall reference. Short, error, mixed-media and obs_recall results are excluded. Storage failures fail open, preserving the original result.
- `obs_recall` pages exact text using **UTF-8 byte offsets**. Start at offset=0 and follow next_offset; pages are not necessarily 4096 bytes. It is not an arbitrary file reader and remains constrained to session archives and paging limits.
- Raw text and an append-only ledger live under the session directory at `sol-pi/<session-id>/observation-pack/{objects,ledger.jsonl}`; history is not rewritten. In-memory/no-session runs use a private random temporary root shared by that session in the process. Shutdown does not delete archives; restart does not restore temporary-root mappings. Deletion breaks recall. Archives may contain sensitive tool output: protect them as session data and clean them up yourself.
- The bundled adapter hides both tools' calls/results (including progress/errors) and plan reminders, without changing model-visible results or native edit/write display. Third-party UIs may show ordinary tool views.

## Configuration and defaults

OCC requires persistent `getSessionFile()` and enabled effective compaction settings. No-session runs hide/deactivate update_plan; `PI_WORKER_DEPTH > 0` registers no OCC tools or handlers. ObservationPack operates independently.

The default [settings resolver](extensions/online-context-compact/settings.ts) reads agent-directory settings.json and **trusted-project** .pi/settings.json, using Pi's effective compaction/retry settings without persisting migrations. SDK hosts using an in-memory SettingsManager, custom agent directory or runtime overrides must inject `resolveSettings` into `createOnlineContextCompactExtension`, returning `{compaction: settingsManager.getCompactionSettings(ctx.model), retry: settingsManager.getRetrySettings()}`. The default cannot observe inaccessible in-memory overrides.

Background settings live at `~/.pi/agent/online-compact-settings.json` (or under `PI_CODING_AGENT_DIR`), reloaded and fixed at each job's start:
```json
{"model":"auto","thinking":"auto","service_tier":"auto"}
```

- All missing values default to auto. Model/thinking independently inherit the main session at job start. Explicit models use `provider/model-id` (IDs may contain /); thinking accepts `off|minimal|low|medium|high|xhigh|max`, subject to model support. Invalid settings warn and skip optimization. SDK hosts may inject `resolveSummarySettings`.
- service_tier accepts `auto|priority|fast|ultrafast`; auto adds no field, others pass literally to the summary payload without provider checks/conversion. Main-session and native /compact requests are unaffected. Unsupported values may fail at the server.
- Factory option `cacheWriteReadRatio` defaults to **12.5**, accepting a nonnegative number or null (no economic-price estimate); it is not part of the JSON above. Gates use a 1,000-token estimated memo, request horizon, removable prefix, accumulated cache debt and cooldown. Window protection can bypass cooldown, using effective reserve tokens. See [economics.ts](extensions/online-context-compact/economics.ts).
- Each selected boundary adds **1–2 model requests plus configured retries**. Summaries disable cache writes; compaction invalidates the main prompt-cache prefix. Prices, savings and UI token deltas are estimates, not billing, budget caps or guarantees. Successful checkpoint usage excludes some failed/cancelled/discarded charged work.
- requestCount/debt repayment count main-agent `turn_start` events (including continuations and possibly aborts before dispatch), not transport retries. Summaries and cache-warming replays are excluded; old counters are not retroactively corrected. Uniquely matched signed same-model encrypted reasoning can receive usage-bounded prefix credit; ciphertext length is never tokenized and unattributed usage stays retained cost. This is only a replay-cost proxy.

## Triggers, replay and safety

Only a **successful current-turn update_plan completing a previously registered unfinished step, with work remaining**, can authorize a boundary. Repeating/renumbering plans, newly completed steps and full-plan completion do not trigger it; economic gates can defer. Nested calls require the SDK's finalized `nestedCalls`, the current-turn update ID and successful outer/intermediate parents, not output text/details. Truncated, failed, blocked or cancelled calls do not authorize; a plan may already have persisted before a parent fails.

An eligible turn_end freezes a snapshot and starts background generation through `ctx.modelRegistry.streamSimple()`, request-time authentication and native compact prompts/retries. A later turn_end / agent_before_settle atomically commits the summary with the latest plan, preserving appended messages. Only remaining work gets a hidden reminder and continue:true. There is no main-session abort, awaited ctx.compact or sendMessage restart; idle results wait for another boundary rather than writing sessions or starting conversations.

- Plan/prediction history survives compaction. A new external task after completion can reset task predictions but retains debt and last-compaction time. Input matching is bounded and run-local. Older SDKs lack source/input IDs: mixed, duplicated or transformed input conservatively skips reset; identical bare messages injected outside input cannot be reliably distinguished. Queued images must also arrive unchanged: delivery-time resizing, conversion or omission that changes the content conservatively preserves the plan rather than resetting on matching text alone.
- Boundary drafts **do not fire session_before_compact, session_compact or native failure hooks**. Disable compaction/OCC if extensions must veto/observe every compaction through those hooks. Manual /compact and overflow recovery remain native.
- State and summary share a proposed batch, reconstructed from the branch after commit. Earlier pure custom drafts survive; prior context edits, custom-message or compaction drafts cause a skip. Later extensions can replace/drop the batch and own their modifications.
- Corrections, cancellation, session/tree/switch/fork/shutdown, model changes and native compaction cancel/invalidate work. Commit guards recheck session, complete captured prefix, generation and main model. Ordinary appended messages/metadata survive; branch summaries, context edits and other compactions invalidate. Late results cannot overwrite newer jobs. Revoked SDK contexts are no longer accessed; detached failures write no diagnostics.
- Empty, truncated, tool-calling, failed or cancelled summaries never become checkpoints and do not disrupt existing scheduling. Raw history is not deleted, but summaries may lose details: inspect original sessions or recall archived observations.
- OCC has no before_provider_request accounting hook. CacheWarmer replays neither append OCC state nor repay debt. Native warming may still append usage and advance the leaf; guards permit those records after the captured prefix, not conflicting context changes. User warming settings are unchanged.
- Notifications distinguish ready from applied. Failures report short reasons/timings; obsolete jobs are silent. Only exception messages are used, with terminal controls removed, best-effort credential/URL redaction and a 300-character limit plus ellipsis; no raw errors or OCC diagnostic logs are stored. Token deltas use the same before/after committed projection estimator, including waiting-period messages/continuation reminders but excluding external system prompts, tool definitions and subsequent OP transforms. Growth is never reported as savings.

Implementation: [OCC entry](extensions/online-context-compact/extension.ts), [ObservationPack](extensions/observation-pack/index.ts), [runtime paths](runtime-paths.ts).

## Third-party UI: observe, do not control compaction

See [pi-ui](../pi-ui/README_EN.md) for common transport. Historical `pi-goui:*` names require no Go frontend. There are no `sol-pi.compact`, `plan.get/set` or `observation.read` remote methods; standard Pi compact is a separate host operation. `get_commands` lists neither tools nor services and cannot detect presence. Do not send /update_plan, /obs_recall or a model prompt to probe availability. Keep the ordinary view until recognized state arrives.

For successful `tool_execution_end` with toolName=update_plan, read result.details (details on historical successful toolResult):
```json
{"boundary":true,"completed_step_ids":["inspect"],"progress_recorded":true,"task_status":"active","plan":[{"id":"inspect","goal":"Inspect inputs","status":"completed"},{"id":"apply","goal":"Apply change","status":"in_progress"}]}
```

plan is a full replacement. Do not reorder it or replace committed state with erroneous call arguments. boundary and related fields describe transitions; task_status=active **does not mean closing the session when all steps finish**.

Query initial/resumed history (minimal response; real entries contain other internal fields):
```json
{"id":"history-1","type":"get_entries"}
```
```json
{"type":"response","id":"history-1","command":"get_entries","success":true,"data":{"leafId":"entry-plan","entries":[{"id":"entry-plan","parentId":null,"type":"custom","customType":"sol-pi-online-context-state-v1","data":{"version":1,"plan":[{"id":"inspect","goal":"Inspect inputs","status":"completed"},{"id":"apply","goal":"Apply change","status":"in_progress"}]}}]}}
```

Follow parentId from leafId and project the latest version=1 state on the active branch, not the newest timestamp in the whole tree; never write it back. entry_appended may carry the same state. Rebuild on session replacement/fork/tree navigation. Unknown versions or missing/invalid data retain generic fallback, never fabricated plans; ignore other internal fields.

With RPC UI loaded, notifications look like:
```json
{"type":"extension_ui_request","id":"notice-1","method":"notify","notifyType":"info","message":"pi-goui:event:v1:{\"name\":\"context.applied\",\"data\":{\"beforeTokens\":12000,\"afterTokens\":7000,\"generationMs\":2400,\"durationMs\":8000}}"}
```

Strip the exact `pi-goui:event:v1:` prefix and parse {name,data}:
- context.ready: {generationMs}, generated but not applied;
- context.applied: {beforeTokens,afterTokens,generationMs,durationMs}, estimates may grow or shrink; generation and total elapsed milliseconds differ;
- context.failed: {durationMs,reason}, preserve context/plan and optionally warn.

Facts require no extension_ui_response, are not durable acknowledgments and have no universal severity. Ignore extra fields and unknown/malformed payloads. Missing notifications do not prove failure. Persisted metrics are active-branch `sol-pi-online-context-metrics-v1` custom entries with estimated:true and applied metrics; do not replay history as new toasts.

Example obs_recall tool arguments and result details (raw text is in content):
```json
{"id":"obs_0123456789abcdef01234567","offset":0}
```
```json
{"id":"obs_0123456789abcdef01234567","offset":0,"bytes":4096,"lines":80,"nextOffset":4096,"eof":false}
```

Details use nextOffset/eof; the text header uses next_offset/eof. Stop at eof=true. There is no dedicated RPC click-to-recall endpoint: do not implicitly prompt a model or read arbitrary host paths from UI. Missing IDs, changed sessions, invalid offsets and deleted archives are ordinary tool errors. Streaming and unknown future tools fall back to generic rendering; internal details are not authorization.

## Compatibility and validation

[native-preparation.ts](extensions/online-context-compact/native-preparation.ts) is a Pi 0.87.1 preparation-only port, adapted for 1.0.2 and historically differential-tested against **1.0.4**. Its `sol-pi-occ-native-0.87.1` checkpoint marker preserves historical file lists ([license](extensions/online-context-compact/LICENSE.pi)). Active-branch projection preserves tool pairing, split turns, previous summaries and cumulative files; nested file records indicate attempts, not successful writes. Production uses public imports, tests may compare private APIs. Revalidate SDK upgrades; repository-dependency test results do not replace acceptance against the actual global SDK used at runtime.

Default `npm test` discovers both extension `.test.ts` and SoL `.test.mjs` suites. Runners no longer block behavior checks behind an exact historical 1.0.4 version gate; all preparation differentials and session assertions remain enabled. The current full SoL suite and strict typechecking pass against repository SDK **1.5.0** and global SDK **1.5.2**. Queued follow-ups reset completed-plan predictions only when their source is confirmed and content/images arrive unchanged; SDK image transformations or omissions conservatively retain state. Tests cover unchanged valid PNG delivery and transformed images in interactive/RPC modes without relaxing source or content validation. This is offline SDK acceptance, not validation of every version or real model.

~~~sh
# From the agent directory, using already installed repository dependencies
npm --prefix extensions run typecheck
npm --prefix extensions test

# Optional: use an already installed global SDK in isolation; no installation
node extensions/sol-pi/tests/run-global.mjs
node extensions/sol-pi/tests/run-global.mjs --typecheck
~~~

Large tool-result line counting, receipt detection and short head/tail excerpts no longer repeatedly split the full text. Full hashes, original archives and archive-integrity checks retain their necessary linear costs. Equivalence regressions cover UTF-8, CRLF, very long lines and damaged archives. Optional comparative benchmark:

```sh
SOL_PI_OBSERVATION_BENCHMARK=1 node --expose-gc --experimental-transform-types --test --test-name-pattern='synthetic OP benchmark' extensions/sol-pi/tests/observation-pack.test.mjs
```

This measures synthetic text processing, not model/network end-to-end latency.

The [runner](tests/run-global.mjs) isolates HOME/agent, copies into a temporary directory and links existing global dependencies without forwarding caller credentials, session markers or NODE_OPTIONS. SOL_PI_GLOBAL_ROOT can override the npm global root; typechecking requires existing TypeScript. Both entry points use offline fake providers: no installs, global settings changes or remote model calls. See [tests/](tests/) for coverage; this is not live billing or full interactive UI certification.

To uninstall, remove the capability directory and its UI registration, then reload. Clean up archives separately; leave other extensions intact.
