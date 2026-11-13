# Worker batches

[中文](README.md) · [Installation](../README_EN.md)

## Copy and register

Copy both complete directories; no shared directory, build step or helper generation is required. Pi 1.5.2 provides SDK dependencies; use Node 22.19+ and your own authentication/settings.
~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/worker" "$DEST/ui/worker"
cp -R "$SRC/worker/." "$DEST/worker/"
cp -R "$SRC/pi-ui/worker/." "$DEST/ui/worker/"
pi --no-extensions -e "$DEST/worker/index.ts" -e "$DEST/ui/worker/index.ts"
~~~
Alternatively load the capability and your own UI entry, not both UI registrations:
~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/worker/index.ts";
export default function (pi: ExtensionAPI) { registerUI(pi); }
~~~
UI is optional; execution/validation remains capability-owned. Unsupported interactions are unavailable, never implicit consent. Add `--mode rpc` for structured RPC UI; common discovery/transport rules are in [pi-ui](../pi-ui/README_EN.md).

## Dispatch contract

[SKILL.md](SKILL.md) defines dispatch policy. Before every child, `prompt.ts` resolves `agents/worker.md` plus `agents/{mode}.md` (`scout/implement/test/review/fix`) relative to the actual module, not a fixed installation path under the agent directory; missing roles fail the task. Keep these files when copying. Configuration and history remain agent-directory-owned. Prompt edits affect subsequent tasks only; the task contract is separate.

Every task requires `objective`, `mode`, explicit `preset: fast|normal|deep` and `sessionMode: new|fork`, selected by the dispatching model, not routinely by the user. Optional `title` is a short meaningful label; blank/missing titles display the full objective, without changing the execution goal.
~~~js
worker({ tasks: [{ mode: "scout", objective: "Trace the call chain", preset: "fast", sessionMode: "fork" }] })
worker({ batchId: "<returned batchId>" })
worker({ batchId: "<returned batchId>", cancel: true })
~~~
- Start accepts 1–12 `tasks`, never legacy `task`. Continue/cancel requires `batchId`; cancel must be true. Unknown/mixed fields fail before admission. Only one unfinished batch per session: another start returns blocked/`active_batch`/`activeBatchId`, without cancelling existing work. Put parallel tasks in one batch.
- Start/continue waits for the whole batch (no polling/list/status-only mode); continuation neither creates tasks nor reloads routing. Responses retain `batchId`, `tasks[].taskId`, `finished` outside compressed `result: {status,results:[…]}`. Results preserve input order; a failed/blocked task does not cancel siblings. At most 64 batches are retained per session.
- Cancel aborts queued/running work and waits for cleanup/lock release; finished cancellation is harmless. Aborting an active start/continue also cancels the batch; a pre-aborted start admits nothing, a pre-aborted continuation cancels existing work. Aborting after return has no effect; cancelling cleanup cannot itself be interrupted.
- Missing information/decision/authorization returns blocked. Children cannot delegate or communicate with users; the main Agent resolves blockers and reviews evidence/full diffs before delivery. No automatic retries. Scope changes require cancel, cleanup and a new complete contract.

## Isolation, paths and lifecycle

`new` uses `--no-session`. `fork` snapshots the parent's active branch at batch admission, including finalized context/compaction/branch summaries but not unrecorded streaming output or inactive siblings. Queued tasks use that same snapshot, including for in-memory parents; parent entries/leaf/file are not mutated. Prefer fork for contextual work; inherited sensitive history goes to the selected model. Each child gets a fresh private temporary session (0600), deleted on completion/start failure/timeout/cancel; SIGKILL can leave sensitive files. History never expands authority: the child's contract, write guard, recursion ban, `--no-skills` and `--no-context-files` remain in force.

- Write modes require `allowedPaths`, relative to `task.cwd`: `file.ts` is exact (including new files), `dir/**` covers descendants; the same applies to `forbiddenPaths`. Existing bare directories/trailing slashes are rejected before any task starts; whitespace matters. `relevantFiles` are read hints, possibly outside cwd, not write grants.
- Children explicitly load the module-local `guard-entry.ts`, independently of global settings or autodiscovery. This entry installs only the write guard, never the Worker scheduler; loading it alongside the full Worker is deduplicated per host. Missing or invalid task policy blocks edit/write.
- Edit/write must satisfy lexical and canonical allowed paths; forbidden paths match both. Explicit alias prefixes work, but symlinks inside globs cannot expand scope. Conflict locks normalize cwd/symlinks/allowed paths; exclusions do not shrink locks. Read-only work conflicts with writers because its read set is undeclared.
- Shared Git worktrees, edit/write guards and path locks are **not a filesystem/bash sandbox or cross-window lock**. Preserve existing changes. Host-derived `changed_files` uses declared scopes for overlapping writers; `observed_changed_files` retains raw Git deltas and risks describe uncertain attribution. Main Agent reviews the complete diff.
- Session-owned slots/locks last through process termination and validation. Independent queued tasks can bypass conflicting work without overtaking earlier conflicts. Lower concurrency changes admission, not running tasks. One fixed task deadline includes Git checks and validation.
- Unix cleanup uses bounded process-group TERM/KILL, including descendants after leader exit and auxiliary Git commands; parallel checks settle all siblings, and nonzero Git exits invalidate evidence. Windows terminates direct children only. Session shutdown/replacement/tree navigation cleans live work rather than resuming it; another session's runtime is unaffected.

## Settings and progress

Use optional UI `/worker_settings` or in-process capability settings APIs. `worker-settings.json` lives in the configured agent directory. Supply all fast/normal/deep model routes: there is no portable default model ID, keyword routing, auto preset, enable switch or manual flag. Runtime defaults: concurrency **3**, timeout **900000 ms**, output **65536 bytes**. Thinking is exactly `off/minimal/low/medium/high/xhigh/max`, subject to backend model support; changing a model does not silently change thinking or downgrade it. Legacy max routing migrates; obsolete automaticDelegationEnabled is removed.

`worker:v1` (discovered via this module's `capabilities.ts`/`api.ts`) exposes `getSettings/getModels/saveSettings/getSession/readTranscript`. Detached session `lookup/history` snapshots and `subscribe` events (`changed/history/batch/closed`) belong only to that session; unsubscribe on close/replacement. Discover at use time; observers cannot block cleanup or own execution.

## Third-party UI

Discover extension-source `worker_settings` and `pi-ui-rpc` before sending them; missing commands/capabilities disable their entries. Do not send unknown slash commands (they may become model input). There are **no worker.start, worker.cancel, worker.settings.get or worker.settings.save RPC methods**. Worker is an Agent tool, not /worker; Pi abort is not a per-batch API.

Tool progress comes from `tool_execution_update.partialResult.details` / `tool_execution_end.result.details` and historical toolResult details:
~~~json
{"kind":"worker-ui","batchId":"batch-1","originToolCallId":"call-1","revision":3,"snapshotAt":1730000001000,"startedAt":1730000000000,"limit":2,"total":1,"completed":0,"tasks":[{"index":0,"mode":"scout","objective":"Inspect the project","status":"running","requestedPreset":"normal","sessionMode":"fork","attempt":1,"phase":"Waiting for a slot","phaseCode":"waiting_slot","activities":[],"toolCalls":0,"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"contextTokens":0,"turns":0},"transcript":{"version":1,"id":"11111111-1111-4111-8111-111111111111"}}]}
~~~

Use `kind=worker-ui`, batchId/originToolCallId/revision, never labels, to merge active-branch latest snapshots (`worker-snapshot-v1` custom entries carry them in data). Ignore stale revisions/abandoned branches. Task status is queued/running/completed/blocked/failed; use stable phaseCode (including waiting_slot, cleanup, cancelling, timed_out), not translated phase text. Unknown codes remain displayable; unknown details fall back to generic tools. Timestamps are epoch ms; freeze history at snapshotAt/finishedAt. Activities/usage are bounded summaries, not full transcripts. Original cards own progress/controlErrors; continue/cancel cards are empty, unknown-batch errors remain visible. Rendering failures cannot strand execution.

### Settings transaction

After discovery:
~~~json
{"id":"settings-open","type":"prompt","message":"/worker_settings"}
~~~

~~~json
{"type":"extension_ui_request","id":"settings-1","method":"input","title":"pi-goui:worker-settings:v1","placeholder":"{\"config\":{\"version\":2,\"fast\":{\"model\":\"provider/model\",\"thinking\":\"low\"},\"normal\":{\"model\":\"provider/model\",\"thinking\":\"medium\"},\"deep\":{\"model\":\"provider/model\",\"thinking\":\"high\"},\"maxConcurrentWorkers\":2,\"defaultTimeoutMs\":300000,\"maxOutputBytes\":65536},\"original\":{\"version\":2,\"fast\":{\"model\":\"provider/model\",\"thinking\":\"low\"},\"normal\":{\"model\":\"provider/model\",\"thinking\":\"medium\"},\"deep\":{\"model\":\"provider/model\",\"thinking\":\"high\"},\"maxConcurrentWorkers\":2,\"defaultTimeoutMs\":300000,\"maxOutputBytes\":65536},\"models\":[{\"id\":\"provider/model\",\"thinking\":[\"off\",\"low\",\"medium\",\"high\"]}]}"}
~~~

Parse placeholder: config is draft, original is committed baseline, models lists exact IDs/thinking choices; error is optional. Example model IDs are placeholders, not defaults. Return the **whole config JSON string**, not a wrapper:
~~~json
{"type":"extension_ui_response","id":"settings-1","value":"{\"version\":2,\"fast\":{\"model\":\"provider/model\",\"thinking\":\"low\"},\"normal\":{\"model\":\"provider/model\",\"thinking\":\"medium\"},\"deep\":{\"model\":\"provider/model\",\"thinking\":\"high\"},\"maxConcurrentWorkers\":2,\"defaultTimeoutMs\":300000,\"maxOutputBytes\":65536}"}
~~~

~~~json
{"type":"extension_ui_response","id":"settings-1","cancelled":true}
~~~

Version 2 requires exactly the shown fields; each route has exactly model/thinking. Integer ranges: concurrency 1–16, timeout 1000–3600000 ms, output 8192–1048576 bytes. Backend schema/model/thinking and atomic save remain authoritative; UI must not directly write settings. Invalid submissions reopen with a new id and error {code,field?,model?,thinking?,message?}; retain draft, replace request id. Codes: invalid-config/invalid-number/model-unavailable/thinking-unsupported/save-failed. Changed successful saves notify `pi-goui:event:v1:` + {"name":"worker.settings-saved","data":{}}; unchanged saves do not. Command failure may notify worker.settings-error {message}; ordinary prompt response is command acknowledgment, not config.

### Read-only transcript RPC

Only use the host task's opaque `transcript:{version:1,id:UUID}`, never model-provided paths. Optional incomplete/error flags are size_limit/io_error/protocol_error.
~~~json
{"id":"rpc-1","type":"prompt","message":"/pi-ui-rpc {\"id\":\"read-1\",\"method\":\"worker.transcript\",\"params\":{\"descriptor\":{\"version\":1,\"id\":\"11111111-1111-4111-8111-111111111111\"}}}"}
~~~

Outer rpc-1 acknowledges the command. A separate extension_ui_request/method notify carries `pi-ui:rpc:v1:` plus:
~~~json
{"id":"read-1","result":{"cursor":"22222222-2222-4222-8222-222222222222","reset":true,"records":[{"type":"worker_transcript_header","version":1,"id":"11111111-1111-4111-8111-111111111111","cwd":"/project","entries":[]},{"type":"message_end","message":{"role":"user","content":"Inspect the project","timestamp":1730000000000}},{"type":"worker_transcript_end","status":"completed","complete":true}],"more":false,"partial":false}}
~~~

Correlate inner read-1, not notification id. Next params are `{descriptor:{version:1,id:"…"},cursor:"RETURNED_CURSOR"}`. Backend validates UUID/version/paths and cursor; never invent offsets/cursors or reuse one across recordings. Reads are capped at 4 MiB; cursor state expires after five idle minutes and may reset after reload/eviction.
- reset:true discards prior replay state before applying ordered JSON-object records. Header carries version/id/cwd/entries; apply subsequent normal Pi events. New entries are empty; old inherited entries remain readable but hidden from this task's view.
- more:true means bytes already remain: continue promptly; more:false is not completion. Poll with bounded delay only while needed. partial:true means the backend retained an incomplete LF-delimited line, not an event. Stop on terminal record, viewer closure or owner backend/session change.
- Errors are {id,error:{code,message}}: unavailable/invalid-transcript/transcript-not-found/transcript-permission/transcript-read-failed/response-too-large. Preserve rendered messages and label gaps; fall back to task snapshots, **not local private-file reads**. Retry explicitly, not indefinitely. Closing/cancelling a viewer never cancels the worker.

## Recording privacy and retention

Logs are separate append-only UTF-8/LF event files, **not native sessions**: never pass them to --session, resume them or open with another writing Agent. Root is `path.join(path.dirname(agentDir()),"worker-transcripts")` (usually ~/.pi/worker-transcripts); agentDir uses PI_CODING_AGENT_DIR or SDK getAgentDir. UUID logs are exclusively created 0600 in a 0700 root; symlink roots/files/escapes are refused. Private permissions are not encryption.

Only task-produced user/assistant/tool messages (including full arguments/results/images/deltas and lifecycle/retry/compaction/custom events) are retained, not inherited fork history, system prompts or environment dumps. New headers have entries:[] in both modes. Missing SDK user events get one exact-task-prompt fallback before output/termination: intended input is not proof a failed spawn executed it. Unknown diagnostics are not another protocol; malformed JSON marks incomplete. Split UTF-8 and partial LF tails must not be treated as whole events.

Terminal status is completed/failed/cancelled/timed_out/incomplete; complete:true means recording finished, not task acceptance. Missing terminal record may mean running or interrupted, never success. Latest terminal marker/descriptor error wins after flush failure. Ordered bounded synchronous writes avoid an unbounded queue, fsync on normal finish; recording IO errors never fail/cancel the worker. Each log caps at **64 MiB** with terminal reserve; stopping reports size_limit rather than silently presenting full history.

Logs survive completion/cancel/session changes. Reader falls back to old agentDir()/worker-transcripts **only on ENOENT** of new root/file, never unsafe/unreadable/malformed data; once a cursor opened the new copy, deletion cannot replay an old same-ID copy. Legacy logs are neither moved/deleted nor counted in the new budget. Missing descriptors cannot be reconstructed; viewers tolerate removed files.

Retention is a **10 GiB soft budget** over regular UUID JSONL files: at/below budget keep all regardless of age/count; above it remove eligible completed/dead-owner logs oldest mtime first (UUID tie-break), rechecking identity/activity. Start/finish requests asynchronous bounded scans; persistent 0600 .retention.lock mtime throttles attempts for 24 hours, including failed scans. No daily expiry, periodic daemon or full-log scans (legacy tails at most 4 KiB). Expired lock takeover is best effort, not distributed coordination.

Exclusive UUID.active markers protect live hosts until execution finishes, even after recording caps. PID plus Linux boot/start identity guards PID reuse; other platforms conservatively retain reused live PIDs. Host finally releases/finishes leases on errors/cancel; dead owners can be reclaimed later, malformed/unverifiable markers retain space. Orphan inactive leases are cleaned; markers protect deletion, never block new tasks/viewers. SIGKILL may leave residue; cleanup only deletes logs when over budget, avoids symlinks/unknown names, and may remain over budget while active/uncertain data is protected.
