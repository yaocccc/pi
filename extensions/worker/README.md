# Worker batches

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

Routes are explicitly selected as `fast/normal/deep`; no keyword or file-count inference is performed. Model and thinking are read exactly from `worker-settings.json`, never converted or downgraded. Thinking supports `off/minimal/low/medium/high/xhigh/max`, subject to model capabilities; invalid or unsupported settings fail explicitly. Changing the model in settings does not automatically change thinking. Delegation is always supported; no enable switch or `manual` parameter is needed. `/worker_settings` edits routing, concurrency, task timeout and output limits. Obsolete `automaticDelegationEnabled` is removed when loading old settings. Legacy top-level max routing is migrated; fast/normal/deep are required. Results retain actual model validation, usage, execution session mode, structured failure categories, Git evidence, secret redaction and bounded compression.

## Model context

`SKILL.md` contains dispatch policy; this README holds runtime and troubleshooting detail. `prompt.ts` builds compact task contracts, omitting empty optional fields and host-owned routing metadata while preserving supplied context, boundaries and existing changes. Workers need not report `changed_files`: the host derives it from Git. `agents/worker.md` retains the safety and mode rules. Budget tests cover tool metadata, Skill and task boilerplate; result compression still preserves validation, acceptance and execution diagnostics.

`runtime.ts` is the sole task scheduler; `security.ts` supplies conflict and Git checks. The process-level slot guard remains for direct process callers and owner cleanup, not as a second task scheduler.

## Progress and history

`presentation.ts` owns history snapshots, refresh registrations and best-effort UI callbacks; `ui.ts` handles rendering and result formatting. Their unit tests stay separate from `background-ui.test.ts`, which exercises real Pi tool-card integration.

The original tool card shows progress, recent tools/thinking, usage, duration and conclusions. Continue/cancel cards render zero lines; valid batch control errors appear on the original card and remain real model-facing errors. Unknown-batch errors remain visible separately. Rendering exceptions cannot strand work.

Detached sanitized snapshots are persisted to session history. Only the active branch's latest snapshot is merged into the original card; historical elapsed time is frozen. Reload, session replacement, tree navigation and shutdown clean up live work rather than resuming it. Runtime and process ownership are session-local; historical cards cannot acquire a new session's live state.

## Verification

The legacy UI is archived unchanged at the repository root `ui/`, outside extension discovery. Active extensions use `extensions/shared/` instead. The optional full legacy UI patch-stack test is skipped while `extensions/ui/index.ts` is absent; other Worker and question tests still run.

```sh
node --experimental-transform-types --test extensions/worker/*.test.ts extensions/shared/*.test.ts extensions/ask-question/*.test.ts
extensions/node_modules/.bin/tsc --noEmit -p extensions/tsconfig.json
git diff --check
```
