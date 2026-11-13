# Worker batches

[中文](README.md) · [Installation](../README_EN.md)

## Copy just this capability and its UI

Copy these **two complete directories**, keeping the relative layout below. No `shared/`, whole repository, top-level `pi-ui/index.ts`, or build step is required. Thin support code is already included.

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/worker" "$DEST/ui/worker"
cp -R "$SRC/worker/." "$DEST/worker/"
cp -R "$SRC/pi-ui/worker/." "$DEST/ui/worker/"
~~~

~~~text
my-extensions/
├── worker/index.ts        # capability, including all its local files
└── ui/worker/index.ts     # optional UI, including all its local files
~~~

**Native Pi, explicit entry points:**

~~~sh
pi --no-extensions -e "$DEST/worker/index.ts" -e "$DEST/ui/worker/index.ts"
~~~

**Or register native UI yourself**, for example in `$DEST/my-ui.ts`:

~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/worker/index.ts";
export default function (pi: ExtensionAPI) {
  registerUI(pi); // does not register/replace the capability
}
~~~

Load the capability and `my-ui.ts` instead of the UI entry above; do not load both registration paths or the whole-package adapter too. With automatic capability discovery, only add the UI registration. Without this UI, the capability still owns execution/validation; unsupported interactions must remain unavailable, never implicit consent.

For RPC, add `PI_UI_PROTOCOL=1` and `--mode rpc` to the explicit-entry command; the detailed wire contract follows below. Worker/Codex UI modules include their own optional method bridge and coordinate to register it once when combined. Other modules do not invent additional methods.

Pi 1.5.2 supplies the SDK, `pi-tui` and `typebox`; you do not need this repository's `node_modules` for these two directories. Use your own authentication, models and capability settings; copying source is not configuration. Keep directory names/layout as shown, or adjust imports yourself. Maintainers sync bundled helpers; consumers **do not** run generation scripts.

## Role prompts

Before each child starts, `prompt.ts` reads `agents/worker.md` (shared boundaries) plus `agents/{mode}.md` (`scout`, `implement`, `test`, `review`, or `fix`) and injects them via `--append-system-prompt`. Both `new` and `fork` use only the selected role; missing files fail the task. The task contract is passed separately. Prompt edits affect subsequent tasks, not running children.

## Task title

Supply an optional `title`: a one-sentence task label written by the dispatching model (aim for 10–30 characters, not a mechanical truncation). Native Pi and Go UI display a nonblank title in place of the objective; omitted or blank titles fall back to the complete `objective` for old calls/history. `objective` remains the full execution goal and is unchanged in the worker contract.

## Read-only task transcripts

New executions also save a private, bounded event log in `~/.pi/worker-transcripts/` (beside the configured agent directory when overridden). Each task carries an opaque `transcript: {version: 1, id}` descriptor for the optional read-only viewer; logs are not resumable sessions and do not alter `new`/`fork` isolation. They retain this task's real messages and tool events, omit inherited fork history and system prompts, and explicitly mark recording failures/size limits. Logs up to 10 GiB total are all retained; above that soft budget, oldest completed/dead-owner logs are removed first while active tasks are protected. Lifecycle-triggered checks use one `.retention.lock` file in the log directory to record the last attempt and skip scans for 24 hours; expired-lock takeover is best effort, not a distributed coordination service. Failed scans retain the timestamp until the next day. The per-task 64 MiB cap remains. Existing agent-local logs are read-only compatibility sources: only an absent new file/root permits fallback, and cleanup never migrates or deletes the old directory. `.active` is a deletion-protection marker, not a lock that can block new tasks: dead-host markers can be reclaimed on a later check, while malformed/uncertain markers conservatively retain storage. See [TRANSCRIPT_EN.md](TRANSCRIPT_EN.md) for storage, privacy and lifecycle. Current frontend access uses capability `readTranscript` through the optional `worker.transcript` RPC bridge; the backend owns path/cursor validation and bounded reads. Go's replay adapter lives under `internal/extensions/worker` and does not read these private files. Existing tasks without a descriptor cannot be reconstructed.

## Task session mode

Every task requires explicit `sessionMode: "new" | "fork"` and `preset: "fast" | "normal" | "deep"`. The dispatching model chooses both; users do not need to configure them. Prefer `fork` to retain the current conversation’s decisions and investigation context. Use `new` only when little context handoff is needed and a short contract fully describes the task. Do not ask the user to select a mode unless they explicitly want control. Omitted or invalid values are rejected; `auto` is not a preset. The following is a model tool-call example, not required user input. Modes may be mixed in a `tasks` batch:

```js
worker({ tasks: [
  { mode: "scout", objective: "Look up one symbol; no prior context needed", preset: "fast", sessionMode: "new" },
  { mode: "review", objective: "Review using our conversation", preset: "normal", sessionMode: "fork" },
] })
```

- **new** retains the existing `--no-session` behavior: no parent conversation history.
- **fork** snapshots the parent's **current active branch at batch admission**, including finalized messages, compaction/context-edit entries and branch summaries. Queued tasks use that same detached snapshot, not later parent turns or inactive sibling branches. In-memory (`--no-session`) parents work too. This is not a switch of the parent's active branch, and does not mutate its entries, leaf or file. Unfinished streaming messages/results not yet recorded by Pi cannot be included.
- Each fork gets a fresh session ID and a private temporary JSONL file (`0600` inside a private directory), passed via `--session` and `--session-dir`, without `--no-session`. It is removed with the task's temporary directory on normal completion, startup failure, timeout or cancellation. An uncatchable host crash/SIGKILL can leave temporary files; inherited history may contain sensitive data and is sent to the task's selected model.
- Forking copies conversation history, **not** the parent's live runtime or permission grants. The child still uses its own task contract, model/thinking route, Worker prompt, write guard, recursion ban and `--no-skills`/`--no-context-files`. Old instructions in history are context, not authority to expand the task. The Git worktree remains shared: session isolation is not a filesystem sandbox.
- Fork tasks show `fork` in the task card; result `execution.session_mode` records the effective mode. Historical cards without the field retain their original appearance.

Implementation uses read-only `ctx.sessionManager.getBranch()` rather than `getEntries()` or `SessionManager.forkFrom(parentFile)`: file-based forking cannot represent an in-memory parent's selected leaf reliably. A fresh `SessionManager.inMemory(cwd).getHeader()` supplies the child header; original branch entry IDs are preserved so compaction and context-edit references still resolve. No parent session file is opened for writing.

## Start, continue and cancel

The parent registers only `worker`:

```js
worker({ tasks: [{ mode: "scout", objective: "Trace the call chain", preset: "fast", sessionMode: "fork" }] })
worker({ batchId: "<returned batchId>" })
worker({ batchId: "<returned batchId>", cancel: true })
```

- Only one unfinished batch per session. Put parallel work in one `tasks` array (1–12 tasks), not separate starts. A second start immediately returns `blocked`, `error: "active_batch"` and `activeBatchId`; it does not cancel the existing work.
- Start requires `tasks` (1–12 items), even for one task; the old `task` field is rejected. Continue/cancel requires `batchId`; `cancel` must be `true`. Unknown or mixed fields fail before admission. Continuations neither create work nor reload routing.
- Start/continue waits for whole-batch completion, without polling. Missing information, decisions or authorization makes a worker return `blocked` directly. The main agent handles the blocker and may dispatch a new complete contract; no automatic retries.
- Responses contain `batchId`, `tasks[].taskId`, `finished` and compressed batch results under `result` (`{ status, results: [...] }`, even for one task). IDs and control state remain outside compression. One failure/blocker does not cancel siblings; results preserve input order.
- Cancel aborts queued/running work and waits for cleanup and lock release. Cancelling an already-finished batch is harmless. Aborting an active start/continue invocation also cancels and cleans up its batch. A pre-aborted start admits no work; a pre-aborted continuation cancels existing work. Aborting after return has no effect. Cleanup cannot be interrupted by aborting a cancel call.
- A session retains at most 64 batches. There is no polling/status-only/list mode.

## Scheduling and safety

The session-owned runtime retains its concurrency and conflict queue across batches; the tool admits only one unfinished batch. Tasks hold slots and path locks through process termination and result validation. Independent tasks can bypass a conflicting queued task without bypassing earlier conflicting work. Reducing concurrency affects admission, not running processes. Each task has one fixed total deadline, including Git checks and result validation.

Write scopes are relative to `task.cwd`: `backend/file.ts` is an exact file, including a new file; `backend/**` covers descendants. The same applies to `forbiddenPaths`. Existing bare directories and any trailing slash are rejected before any batch task starts. Nonexistent bare paths can be exact new files. `relevantFiles` are read hints only and may be outside cwd. Scope checks guard edit/write, not bash or other tools: this is not a filesystem sandbox.

Conflict checks conservatively normalize cwd, symlinks and all allowed paths; exclusions do not shrink lock scope. Edit/write must match both the lexical and canonical allowed paths. Explicit alias prefixes remain supported, but links inside a glob cannot expand its canonical scope; forbidden paths match both lexical names and canonical targets. Filename whitespace is significant. Read-only work conflicts with writers because its read set is not declared. Shared worktrees are not isolated: for overlapping writers, `changed_files` is attributed to declared paths, while `observed_changed_files` preserves the raw Git delta and risks explain uncertain attribution. The main agent must review the complete diff and preserve pre-existing changes.

Changing scope requires cancellation, cleanup and a new contract. Child workers cannot delegate or communicate with the user. Missing permissions are blockers, never implicit authorization.

## Processes, routing and results

`process.ts` launches a child with stdin ignored and only stdout/stderr pipes. Stdout is Pi's JSON event stream. Each session owns its children, slots, waiters and shutdown state; shutting down one session does not stop another session's processes. Unix termination targets process groups, with bounded TERM/KILL cleanup, including descendants left after a normal leader exit (even with inherited output pipes). Auxiliary Git commands also await group cleanup; parallel Git checks settle all siblings before returning an error, and nonzero Git exits invalidate evidence. Windows retains direct-child-only termination. Slot waits, task timeouts, output limits and shutdown preserve diagnostic sources. Separate sessions may still share files: locks are not cross-window locks.

Routes are explicitly selected as `fast/normal/deep`; no keyword or file-count inference is performed. Model and thinking are read exactly from `worker-settings.json`, never converted or downgraded. Thinking supports `off/minimal/low/medium/high/xhigh/max`, subject to model capabilities; invalid or unsupported settings fail explicitly. Changing the model in settings does not automatically change thinking. Delegation is always supported; no enable switch or `manual` parameter is needed. The optional `extensions/pi-ui` adapter registers `/worker_settings` to edit routing, concurrency, task timeout and output limits; headless clients use the settings capability. Obsolete `automaticDelegationEnabled` is removed when loading old settings. Legacy top-level max routing is migrated; fast/normal/deep are required. Results retain actual model validation, usage, execution session mode, structured failure categories, Git evidence, secret redaction and bounded compression.

## Model context

`SKILL.md` contains dispatch policy; this README holds runtime and troubleshooting detail. `prompt.ts` builds compact task contracts, omitting empty optional fields and host-owned routing metadata while preserving supplied context, boundaries and existing changes. Workers need not report `changed_files`: the host derives it from Git. `agents/worker.md` retains the safety and mode rules. Budget tests cover tool metadata, Skill and task boilerplate; result compression still preserves validation, acceptance and execution diagnostics.

`runtime.ts` is the sole task scheduler; `security.ts` supplies conflict and Git checks. The process-level slot guard remains for direct process callers and owner cleanup, not as a second task scheduler.

## Progress and history

`data.ts` owns frontend-neutral sanitization, bounded result/activity data and usage accounting; `history.ts` restores active-branch snapshots, and `callbacks.ts` isolates best-effort telemetry callbacks. The entire Worker capability, including tool registration, is independent of rendering and UI interaction. Native rendering, row refresh and settings menus live in `extensions/pi-ui/worker/`, whose `registerWorkerUI(pi)` uses the SDK's independent tool-renderer resolver. RPC/Go consumes the same `worker-ui` details and renders independently. Refresh timers start only when native tool rows render; initial history rendering before `session_start` is preserved.

`api.ts` defines the `worker:v1` capability, discovered with `getCapability<WorkerCapability>(pi, WORKER_CAPABILITY)` from this module's `capabilities.ts`. It exposes `getSettings()`, `getModels(ctx)`, authoritative `saveSettings(ctx, value)`, `getSession()`, and bounded `readTranscript(request)` for read-only recorded events. `readTranscript` validates descriptors/paths and returns an opaque cursor with records and `reset`/`more`/`partial` flags; adapters must not reinterpret its storage layout. A session view supplies detached `lookup(batchId)`/`history(batchId)` snapshots and `subscribe(listener)`, returning an unsubscribe function. Changes are `changed`, `history`, `batch` (with `batchId`), and `closed`. Retain a view only for its own session; a replacement session has a different view. Observers cannot prevent execution or cleanup. UI adapters discover the service at use time, so extension load order does not matter. No module-global service registry or runtime ownership is transferred to a UI.

The original tool card shows progress, recent tools/thinking, usage, duration and conclusions. Continue/cancel cards render zero lines; valid batch control errors appear on the original card and remain real model-facing errors. Unknown-batch errors remain visible separately. Rendering exceptions cannot strand work.

Detached sanitized snapshots are persisted to session history. Only the active branch's latest snapshot is merged into the original card; historical elapsed time is frozen. Reload, session replacement, tree navigation and shutdown clean up live work rather than resuming it. Runtime and process ownership are session-local; historical cards cannot acquire a new session's live state.

## Verification

This checkout does not ship the legacy root `ui/`. Active native adapters live under `extensions/pi-ui/<module>/`; each capability and UI pair includes its own runtime helpers. `shared/` provides maintenance templates and whole-package compatibility, not a copy-install dependency. The optional full legacy UI patch-stack test is skipped while `extensions/ui/index.ts` is absent; other Worker and question tests still run.

```sh
node --experimental-transform-types --test extensions/worker/*.test.ts extensions/pi-ui/worker/*.test.ts extensions/shared/*.test.ts extensions/ask-question/*.test.ts
extensions/node_modules/.bin/tsc --noEmit -p extensions/tsconfig.json
git diff --check
```

## Third-party UI quick start (self-contained)

### Start, prerequisites and discovery
~~~sh
PI_UI_PROTOCOL=1 pi --mode rpc --no-extensions \
  -e /path/to/my-extensions/worker/index.ts \
  -e /path/to/my-extensions/ui/worker/index.ts
~~~
Use your own Pi credentials/settings. Keep all files inside the two copied directories; do not copy just index.ts. Node 22.19+ and the currently tested Pi 1.5.2 host are the baseline. PI_UI_PROTOCOL=1 opts into structured extension UI; the historical PI_GOUI_PROTOCOL=1 alias and pi-goui:* wire names remain compatible. No Go frontend is required. On older adapters that do not yet recognize PI_UI_PROTOCOL, replace that environment assignment with PI_GOUI_PROTOCOL=1; the wire payloads below are identical.

Configure Worker models/presets in your own worker-settings.json first; the UI must not write that file itself or assume the author's provider/model IDs exist. Model execution has its normal permissions/costs. The examples below are UI reads/settings, not requests to launch a Worker.

First send this standard Pi request; match its response by id:

~~~json
{
  "id": "discover",
  "type": "get_commands"
}
~~~

Inspect response.data.commands for the required command with source="extension". A same-named prompt template/skill is not sufficient. If absent, hide the control; **do not send an unknown slash command**, because Pi may treat it as model input. Discovery confirms the adapter command, not every underlying capability.
Use worker_settings for the settings dialog and pi-ui-rpc for recording reads. There is **no remote worker.start, worker.cancel, worker.settings.get or worker.settings.save method**. The worker tool is an agent tool, not a slash command; do not send /worker or /worker.cancel. Tool invocation/control remains with Pi/the agent. Standard Pi abort cancels the current agent run; it is not a targeted Worker-batch API.

### First render tool progress, without additional methods
Listen to tool_execution_start/update/end for toolName="worker". Read update.partialResult.details and end.result.details; historical toolResult messages also contain details. Accept kind="worker-ui", for example:
~~~json
{
  "kind": "worker-ui",
  "batchId": "batch-1",
  "originToolCallId": "call-1",
  "revision": 3,
  "snapshotAt": 1730000001000,
  "startedAt": 1730000000000,
  "limit": 2,
  "total": 1,
  "completed": 0,
  "tasks": [
    {
      "index": 0,
      "mode": "scout",
      "objective": "Inspect the project",
      "status": "running",
      "requestedPreset": "normal",
      "sessionMode": "fork",
      "attempt": 1,
      "phase": "Waiting for a slot",
      "phaseCode": "waiting_slot",
      "activities": [],
      "toolCalls": 0,
      "usage": {
        "input": 0,
        "output": 0,
        "cacheRead": 0,
        "cacheWrite": 0,
        "contextTokens": 0,
        "turns": 0
      },
      "transcript": {
        "version": 1,
        "id": "11111111-1111-4111-8111-111111111111"
      }
    }
  ]
}
~~~

These are detached snapshots, not patches. Key the card by backend/session + batchId and bind it to originToolCallId. A later continuation tool call does not create another start card. Ignore older revision values (fall back to snapshotAt for old data), and never attach a late event to the newly selected session. Retain the last good result when details are absent.

Fields: startedAt/finishedAt/snapshotAt and activity at are epoch milliseconds; limit/total/completed are batch counters. Task status is queued/running/completed/blocked/failed; index identifies the task within its batch. Tasks may additionally contain title, modelId, thinking, resolvedPreset, timestamps and result. activities entries have id,type (phase/tool/thinking),status,label,at and optional phaseCode/detail/contextSizeEstimate. usage is numeric input/output/cacheRead/cacheWrite/contextTokens/turns. controlErrors contains {toolCallId,message}; payload is the model-facing result, not a prerequisite for card rendering.

phase and activity label are presentation text. Use phaseCode for logic: waiting_slot/slot_acquired identify slot activity; other current codes are queued, running, routing, snapshot_workspace, workspace_snapshotted, starting, validating, cleanup, cancelling, terminating_timeout, terminating_output, completed, blocked, failed, cancelled, timed_out. Unknown/missing codes must remain displayable; never parse Chinese text. Freeze historical elapsed time at snapshotAt/finishedAt. Unknown details versions fall back to generic tool rendering.

For history via get_entries/get_tree, project the **active branch** only. Optional custom entries with customType="worker-snapshot-v1" contain the same details in data; match them to the original worker tool call and batch, rather than combining abandoned branches or guessing from titles.

### Settings UI: one structured input transaction
Discover worker_settings, then send {"id":"settings-command","type":"prompt","message":"/worker_settings"}. The command waits for idle. The structured request is:
~~~json
{
  "type": "extension_ui_request",
  "id": "settings-1",
  "method": "input",
  "title": "pi-goui:worker-settings:v1",
  "placeholder": "{\"config\":{\"version\":2,\"fast\":{\"model\":\"provider/model\",\"thinking\":\"low\"},\"normal\":{\"model\":\"provider/model\",\"thinking\":\"medium\"},\"deep\":{\"model\":\"provider/model\",\"thinking\":\"high\"},\"maxConcurrentWorkers\":2,\"defaultTimeoutMs\":300000,\"maxOutputBytes\":65536},\"original\":{\"version\":2,\"fast\":{\"model\":\"provider/model\",\"thinking\":\"low\"},\"normal\":{\"model\":\"provider/model\",\"thinking\":\"medium\"},\"deep\":{\"model\":\"provider/model\",\"thinking\":\"high\"},\"maxConcurrentWorkers\":2,\"defaultTimeoutMs\":300000,\"maxOutputBytes\":65536},\"models\":[{\"id\":\"provider/model\",\"thinking\":[\"off\",\"low\",\"medium\",\"high\"]}]}"
}
~~~

Parse placeholder as JSON. config is the editable draft, original is the committed starting state, models supplies exact available IDs and allowed thinking choices, and error is optional. The provider/model string here is illustrative: use the actual backend models list. Return the **whole config directly as a JSON string**, not {config:…}:
~~~json
{
  "type": "extension_ui_response",
  "id": "settings-1",
  "value": "{\"version\":2,\"fast\":{\"model\":\"provider/model\",\"thinking\":\"low\"},\"normal\":{\"model\":\"provider/model\",\"thinking\":\"medium\"},\"deep\":{\"model\":\"provider/model\",\"thinking\":\"high\"},\"maxConcurrentWorkers\":2,\"defaultTimeoutMs\":300000,\"maxOutputBytes\":65536}"
}
~~~

version must be 2, with exactly fast/normal/deep/maxConcurrentWorkers/defaultTimeoutMs/maxOutputBytes plus version. Each preset has exactly model and thinking. Integers: concurrency 1–16, timeout 1,000–3,600,000 ms, output 8,192–1,048,576 bytes. Backend model/thinking validation remains authoritative; UI hints are not permission to bypass it.

Invalid submissions reopen the input with a new request id and error {code,field?,model?,thinking?,message?}. Codes: invalid-config, invalid-number, model-unavailable, thinking-unsupported, save-failed. Replace the active request id on retry and preserve the supplied draft. Changed successful saves emit pi-goui:event:v1: + {"name":"worker.settings-saved","data":{}} in a notify message; unchanged saves do not emit that fact. Command-level failures may emit worker.settings-error with {message}. The normal prompt response acknowledges/finishes the command, not a serialized config result.

Cancel:
~~~json
{
  "type": "extension_ui_response",
  "id": "settings-1",
  "cancelled": true
}
~~~
Without the structured marker, the adapter uses ordinary select/input dialogs; a generic RPC UI can support them without a custom settings form.

### Optional read-only transcript viewer
Use only a task's transcript {version:1,id}; incomplete/error may additionally indicate size_limit/io_error/protocol_error. It is an opaque UUID, not a file path or resumable session. After discovering pi-ui-rpc:
~~~json
{
  "id": "rpc-1",
  "type": "prompt",
  "message": "/pi-ui-rpc {\"id\":\"read-1\",\"method\":\"worker.transcript\",\"params\":{\"descriptor\":{\"version\":1,\"id\":\"11111111-1111-4111-8111-111111111111\"}}}"
}
~~~

The ordinary response id rpc-1 is only the command acknowledgment. A separate extension_ui_request with method notify contains message="pi-ui:rpc:v1:" followed by this JSON:
~~~json
{
  "id": "read-1",
  "result": {
    "cursor": "22222222-2222-4222-8222-222222222222",
    "reset": true,
    "records": [
      {
        "type": "worker_transcript_header",
        "version": 1,
        "id": "11111111-1111-4111-8111-111111111111",
        "cwd": "/project",
        "entries": []
      },
      {
        "type": "message_end",
        "message": {
          "role": "user",
          "content": "Inspect the project",
          "timestamp": 1730000000000
        }
      },
      {
        "type": "worker_transcript_end",
        "status": "completed",
        "complete": true
      }
    ],
    "more": false,
    "partial": false
  }
}
~~~

Correlate inner id read-1; ignore the outer notification id. Next call uses params {descriptor:{version:1,id:"…"},cursor:"RETURNED_CURSOR"}. Never compute byte offsets, manufacture a cursor, or use one from another recording. Each call reads at most 4 MiB of file bytes; complete records can span reads. Cursor state is bounded and expires after five minutes of inactivity; reload/eviction can cause a reset.

On reset:true discard the old replay state before applying records. records are JSON objects in order, **not JSON strings**: the first is worker_transcript_header with version/id/cwd/entries, followed by ordinary Pi event objects (message_start/update/end, tool_execution_*, entry_appended, etc.), and optionally worker_transcript_end. New headers have entries:[]; older headers may contain inherited entries, which must not be displayed as this task's work. The end record has status completed/failed/cancelled/timed_out or incomplete and complete:boolean; an incomplete end may include error. Missing end is not proof of success.

more:true means unread bytes already existed: continue promptly. more:false is not “task complete”; poll with a bounded delay while the task runs. partial:true means an unfinished JSON line is retained by the backend, not a completed partial event. Stop polling when the recording ends, the viewer closes, or its owner backend/session changes.

RPC errors are {id,error:{code,message}}, including unavailable, invalid-transcript, transcript-not-found, transcript-permission, transcript-read-failed and response-too-large. Keep already rendered messages and label incompleteness; with no messages/descriptor/capability, show the existing task snapshot instead. Do not fall back to reading local private files. Canceling a read/viewer stops local waiting only, not Worker execution. Use explicit user retry rather than an unbounded error loop.

### In-process frontend alternative
These methods exist only inside the same Pi process, not as additional remote names:
~~~ts
import { getCapability } from "/path/to/my-extensions/worker/capabilities.ts";
import { WORKER_CAPABILITY, type WorkerCapability } from "/path/to/my-extensions/worker/api.ts";
const service = getCapability<WorkerCapability>(pi, WORKER_CAPABILITY); // "worker:v1"
if (service) {
  const config = service.getSettings();
  const choices = service.getModels(ctx); // [{id,thinking:[…]}]
  // Explicit user save only: service.saveSettings(ctx, editedConfig)
  const view = service.getSession();
  const stop = view.subscribe(change => {
    // changed/history/batch/closed; batch includes batchId.
    // Render view.lookup(selectedBatch) ?? view.history(selectedBatch).
    // On closed, freeze this view and dispose its timers/listeners.
  });
}
~~~

A view has awaitingHistory/closed plus detached lookup/history snapshots. Retain the owning view; a replacement session gets another view. readTranscript({descriptor,cursor?}) returns the same result shape above. SettingsError exposes structured data, not a stable translated message. Observe shared facts via pi.events.on("pi:frontend-event:v1", ({context,event}) => …), filtering event.name="worker.settings-saved"; context identifies the owning session. Do not emit a second save fact from your UI.

**Minimal UI:** generic worker result → snapshot card → settings transaction → optional transcript replay → owner/revision/cancellation cleanup. v1 names are version markers, not proof a future unknown payload is compatible.
