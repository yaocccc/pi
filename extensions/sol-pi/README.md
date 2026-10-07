# SoL-Pi: ObservationPack + Online Context Compact

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
- Each selected boundary spends **one or two additional model requests**, plus configured retries; split-turn history and prefix summaries are validated separately. Summary requests disable cache writes (`cacheRetention: "none"`). Compaction also invalidates the main prompt-cache prefix.
- OCC's persisted `requestCount` and request-horizon/debt-repayment estimates now count **main-agent `turn_start` events**, not provider transport attempts. Each started main turn (including the continuation after OCC) contributes once; retries within that turn do not contribute separately, and a turn aborted before dispatch may still count. Context growth is sampled at turn start. Summary requests and cache-warming replays never contribute. Existing saved counters remain usable but are not retroactively corrected.
- Successful checkpoint usage is stored on the compaction entry. Failed, cancelled, retried, or subsequently discarded summary work can still incur provider charges not fully represented by that successful entry. Economics is an estimate, not accounting or a budget cap.

### Compatibility and safety

Pi's preparation function is not publicly exported. `native-preparation.ts` originated as a **preparation-only port of Pi 0.87.1** and was **reviewed and adapted for Pi 1.0.2**, with differential tests rerun against **Pi 1.0.4**, with its [MIT license](extensions/online-context-compact/LICENSE.pi). It uses public projection APIs and the real active branch, preserving context edits, split turns, tool/result pairing, previous summaries, and cumulative file lists, including Pi 1.0.2 nested file calls. OCC retains the persisted `sol-pi-occ-native-0.87.1` format marker so existing `fromHook` checkpoints still carry file lists into subsequent OCC/manual compactions. Production uses only public package imports; private native comparison imports are test-only. Revalidate this port before moving to another Pi version; repository-local dependencies are **not a supported validation target**.

Boundary draft commits **do not fire `session_before_compact`, `session_compact`, or native compaction-failure hooks**. Extensions relying on those hooks to veto/observe all compactions will not intercept OCC; disable compaction/OCC if that is required. Manual/native compaction still uses normal hooks, and OCC reconciles it without double-counting. State is proposed in the same draft batch, never applied speculatively, and is reconstructed from the committed branch before subsequent operations. Earlier pure `custom` drafts are retained; earlier context-editing, custom-message, or compaction drafts cause OCC to skip. Later extensions can replace/drop the proposed batch and own any edits they make to it.

Aborted, deferred, empty, truncated, or tool-calling summaries never become checkpoints. Failures leave the existing scheduler alone. An independent controller cancels background work on corrections, aborted boundaries, session/tree/switch/fork/shutdown, model changes, or native compaction. Session identity, the complete captured branch prefix, generation, and main model are rechecked before returning drafts. Ordinary appended messages and metadata are preserved; branch changes, other compactions, branch summaries, or context edits invalidate the job. Late results cannot overwrite a newer job. OCC does not delete raw history. Summaries can still lose detail; inspect the original session or use `obs_recall` for archived observations when necessary.

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

## Background compaction settings / 后台压缩配置

`~/.pi/agent/online-compact-settings.json` (or the same file under `PI_CODING_AGENT_DIR`):

```json
{
  "model": "auto",
  "thinking": "auto",
  "service_tier": "auto"
}
```

`auto` 分别沿用任务启动时主会话的模型和思考强度。指定模型使用 `provider/model-id`（模型 ID 可包含 `/`）；`thinking` 可设为 `off`、`minimal`、`low`、`medium`、`high`、`xhigh`、`max`，具体支持情况取决于模型。每次启动后台任务时重新读取，不修改主会话设置；缺省文件/字段视为 `auto`，无效配置跳过本次优化并提示。

`service_tier` 可设为 `auto`（默认）、`fast`、`ultrafast`。`auto` 不向压缩请求附加该字段；其余值通过后台摘要请求的 payload 钩子原样附加，例如 `"service_tier": "fast"`，不映射为 `priority`，也不检查模型或供应商。配置在每个任务启动时读取并固定，不影响主会话或原生 `/compact`；实际是否支持加速由服务端决定，不支持的参数可能导致本次后台摘要失败。

Model/thinking `auto` independently inherits the main session's corresponding setting at job start. Explicit models use `provider/model-id`; settings are reloaded for each job without changing the main session. Missing settings default to `auto`; invalid settings fail open with a warning. `service_tier` accepts `auto`, `fast`, or `ultrafast`: `auto` adds nothing, while other values are passed literally in the background summary payload, without provider/model checks or conversion to `priority`. This does not affect the main session or native `/compact`; server-side support is required. SDK hosts can inject `resolveSummarySettings` for their own configuration location.

不显示原生压缩进度、不阻塞对话，也不接管 `/compact` 或 overflow 恢复。摘要就绪时显示生成耗时，确认提交后显示 token 变化和耗时，例如「上下文已压缩：约 32,000 → 8,000 tokens（减少 24,000，75.0%）；生成 3.2s，总耗时 8.4s（含等待安全边界）。新增消息已保留。」；失败提示也显示已耗时，不报告压缩成功。已失效/被取消的任务不报失败。空闲时完成的摘要会等待下一次安全边界，不会自行发起对话或直接写会话。

No native compaction spinner or blocking wait is introduced; manual `/compact` and overflow recovery remain native. Notifications distinguish **summary ready** from **actually applied**, showing generation time and total elapsed time (including the wait for a safe boundary). An idle result waits for a later safe boundary; it never starts a conversation or writes the session on its own. Obsolete jobs are discarded silently.

Token 变化使用同一内容估算方法比较实际提交边界前后的会话投影，包含后台期间新增的消息和本次继续提醒，排除提交之后的新消息。这是估算值，不是 provider 实测或计费用量；不包含会话外的系统提示、工具定义或后续 OP 转换，所以可能不同于界面上的上下文计数。摘要更大时会如实显示「增加」。

Token deltas compare the committed session projection before/after compaction with the same content estimator, including intervening messages and the continuation reminder, but not later messages. These are estimates, not billed/provider-measured usage; out-of-session system prompts, tool definitions and subsequent OP transforms are excluded. Growth is reported as growth, not savings.

## Offline validation

Run from the agent directory:

```sh
node extensions/sol-pi/tests/run-global.mjs
node extensions/sol-pi/tests/run-global.mjs --typecheck
```

The reusable runner requires global Pi **1.0.4**, copies only this extension into a temporary directory, and symlinks **existing** global dependencies there. It never changes installed `node_modules`, manifests, or lockfiles. `SOL_PI_GLOBAL_ROOT` can override `npm root -g`. Typechecking uses an existing global TypeScript compiler (directly installed or bundled with global ts-node), and never installs one.

The offline suite includes three native CacheWarmer regressions: idle/streaming replay isolation with carried debt, successful summary commits during warming with normal main continuation counts, and rejection of conflicting context edits hidden behind warming usage. Tests trigger the pinned Pi 1.0.4 CacheWarmer's real refresh directly (test-only private access), using the SDK callback chain and authenticated faux provider without waiting for timers. Other cases cover OP/OCC registration without native-tool overrides, observation packing and recall, real AgentSession + authenticated faux-provider runs with one/two compactions before the original prompt resolves, exact request/settlement counts, failure/cancellation/retry handling, final-plan and worker/no-session exclusion, default economic gates, state reopening/manual compaction, draft interference, native preparation parity, project trust, and lifecycle invalidation. No remote model calls are made. Tree/switch/fork/shutdown cancellation handlers also have focused unit coverage; this is not a live-provider billing or full interactive-UI certification.

Verified on global **Pi 1.0.4**: **174/174 tests pass in 10 files**, with no skips/cancellations, and strict TypeScript checking passes. Coverage includes non-blocking generation, idle-to-next-run application, new-message preservation, native manual/overflow preemption, model/thinking configuration, honest notifications with boundary-local token deltas and separate generation/total timings, native-preparation differential/legacy-marker checks, nested tool calls and successful ancestry, projected-prefix economics and cumulative debt, plan restatement/cooldown/task handoff, real queued follow-ups and cancellation, and persistent/ephemeral OP archives with byte-exact UTF-8 recall and fail-open storage errors. No remote model calls, real credentials, dependency installs, or global settings changes are needed.

Archived observations remain local and are not automatically deleted.
