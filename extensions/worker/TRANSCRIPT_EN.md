# Worker transcript v1 (read-only viewer protocol)

[中文](TRANSCRIPT.md)

The Worker CLI/session isolation is unchanged: `new` still uses `--no-session`; `fork` still executes against a temporary captured branch which is deleted after exit. The transcript is a **separate append-only event log**, not a native Pi session and must never be passed to `--session`, resumed, or opened by a second writing agent.

## Discovery and lifetime

- Root: `path.join(path.dirname(agentDir()), "worker-transcripts")`, normally `~/.pi/worker-transcripts` (beside `~/.pi/agent`, not inside it). `agentDir()` is the existing Worker configuration resolver: `PI_CODING_AGENT_DIR` when set, otherwise SDK `getAgentDir()` (path-resolved). New writers and retention use only this sibling directory; `.retention.lock` and `<UUID>.active` live beside its logs. The backend reader, exposed to Go through `worker.transcript` RPC, first tries this location and falls back to the old `agentDir()/worker-transcripts` only when the new root or file is absent (ENOENT). Unsafe, unreadable or malformed new records never trigger fallback. Once a cursor has opened the new copy, deletion does not make it replay an older same-ID copy. Old files are neither moved nor deleted automatically and are not included in the new directory's capacity budget; UUID descriptors remain unchanged.
- Task field: `transcript: { version: 1, id: "<UUID>" }`. This is published in `ChildProgress`, returned in `ChildResult`/host task result, copied into `WorkerUiTask`, and retained in `worker-snapshot-v1` parent history. An optional `incomplete: true, error: "size_limit" | "io_error" | "protocol_error"` flags known recording failure; the descriptor never contains a filesystem path.
- Resolve **only a validated UUID** to `<root>/<id>.jsonl`; reject unknown versions, path traversal, symlink files and symlink escape. Do not accept an arbitrary path from task text/results. Use the host-produced task descriptor rather than a model-returned value.
- Files are created exclusively with random UUID names, mode `0600`; root mode `0700`. Existing symlink roots are refused. Permissions are private, not encryption: real task messages/tool results may contain sensitive data.
- Logs survive task completion/cancellation and parent session switches, subject to the size-budget retention below. Old tasks without a descriptor have no recoverable record. Viewers must tolerate missing files, including an old completed log removed while its read-only detail is open.

## JSONL framing and replay

UTF-8 records delimited by **LF**, not Unicode U+2028/U+2029. Readers must retain a partial last line while a task is running; do not rescan the entire log on each frame. Native JSON stdout chunks are decoded with `StringDecoder`, so split multibyte characters survive.

First record:

```json
{"type":"worker_transcript_header","version":1,"id":"12345678-1234-4234-8234-123456789abc","cwd":"/project","entries":[]}
```

New recordings always write `entries: []`, for **both `new` and `fork`**. The execution fork still receives its captured branch through its temporary native session; that inherited history is not copied into the viewer log. Only events produced by this task are recorded. Existing v1 logs with populated `entries` remain readable without rewriting: initialize the viewer with the header via `State.LoadEntries`, retain the inherited-message boundary, and hide inherited entries from the task view. Set cwd from the header separately; then send subsequent session-event lines to `State.Apply`.

Subsequent rows are real JSON-mode session events, using the same shapes as RPC `State.Apply`:

```json
{"type":"agent_start"}
{"type":"message_start","message":{"role":"user","content":"actual task prompt","timestamp":1}}
{"type":"message_end","message":{"role":"user","content":"actual task prompt","timestamp":1}}
{"type":"message_start","message":{"role":"assistant","content":[],"timestamp":2}}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"Working…"}}
{"type":"tool_execution_start","toolCallId":"call-1","toolName":"read","args":{"path":"file.go"}}
{"type":"tool_execution_end","toolCallId":"call-1","toolName":"read","result":{"content":[{"type":"text","text":"complete output"}]},"isError":false}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Finished"}],"timestamp":3}}
{"type":"agent_end","messages":[]}
{"type":"agent_settled","aborted":false}
{"type":"worker_transcript_end","status":"completed","complete":true}
```

The native `session` header is not repeated. System-message events/checkpoints are omitted; all normal user/assistant/tool content, including streaming deltas, tool arguments, complete results, and images, is preserved without the Activity UI's summarization. Known session lifecycle, retry, compaction and custom-entry events are retained. Unknown diagnostic stdout records are not a second protocol; malformed JSON marks the recording incomplete.

The SDK normally emits the real user start/end events. For older/failing launchers that do not, the writer inserts standard `message_start`/`message_end` user events containing the exact supplied task prompt before the first assistant/tool event (or before termination if no messages arrive). This fallback records intended input, not a claim that a failed spawn executed it. It does not duplicate normal SDK user events. No environment dump or separately supplied Worker system prompt is recorded.

Terminal record:

- `status`: `completed`, `failed`, `cancelled`, `timed_out`, or `incomplete`.
- `complete: true` means recording finished normally, **not** that the task succeeded. Only `status: completed` represents successful process completion; task-level acceptance/result status can still differ.
- `complete: false` plus `error` means recording stopped early. Missing terminal record after a crash also means interrupted/incomplete, never successful completion. A still-running log normally has no terminal record yet. The latest terminal marker and descriptor error flag win if a final flush failed.

## Bounds and failures

Each task log is capped at 64 MiB, with a small terminal-marker reserve. Once the cap is reached, recording stops and both descriptor and terminal marker explicitly report `size_limit`. Inherited branch size no longer consumes any recording budget. No hidden truncation is presented as full history.

Synchronous bounded appends preserve event order and avoid an unbounded in-memory promise/write queue. The file is visible immediately to a read-only incremental viewer; normal completion fsyncs then closes it. IO errors never fail/cancel the Worker itself. On disk failure a final marker may be impossible, so the descriptor carries the error and readers must treat a missing/partial tail conservatively. Hard process death cannot guarantee a final marker.

## Automatic retention

- Budget: **10 GiB = 10 × 1024³ bytes**, summed over regular UUID-named `*.jsonl` files in this directory. At or below the budget, every log is retained regardless of age/count. Only when over budget are eligible logs deleted, oldest **last-modified time** first (UUID breaks ties), until the scanned total fits. There is no daily expiry or count limit.
- Checks are requested only when a recording starts or its task finishes. A single persistent `.retention.lock` file (0600) in the log directory records the last attempted scan through its modification time. A fresh lock skips enumeration for **24 hours**, including across midnight; failures leave the timestamp in place so they do not trigger repeated scans that day. A later lifecycle event may replace an expired lock with exclusive creation (`wx`); a process-local throttle also avoids repeated lock checks. Expired-lock replacement is best effort, not a distributed-lock protocol: simultaneous expiration takeovers may briefly overlap. Collectors recheck lock ownership and each log's identity/activity before deletion. Old time-bucket markers are ignored; no new ones are created. Scanning runs asynchronously, yields while inspecting files/deleting batches, and reads metadata plus at most the last 4 KiB of legacy/completed logs, never their full contents. No per-event scan, periodic timer, daemon, or unbounded work queue is added.
- Writers exclusively publish `<UUID>.active` before creating the log and keep it until task execution finishes, even if recording hits its 64 MiB cap earlier. Leases carry the hosting process PID and, on Linux, boot ID plus process-start ticks to guard PID reuse/reboots. Other collectors skip live owners regardless of log age. Crash leases become reclaimable when the owner dies; non-Linux platforms conservatively protect a reused live PID until it exits. Normal complete recording removes the lease; if recording stopped early or a terminal write fails, a finished lease allows reclamation without waiting for the parent Pi process to exit. Orphan inactive leases are cleaned on scans. An `.active` file is a deletion-protection marker, not a mutex: stale or malformed markers cannot block creating another task or reading its details. Child failure/cancellation and synchronous startup exceptions release or finish the marker in the host's `finally` path, even while the host remains alive. An uncatchable host crash can leave a marker; a later scheduled scan detects its dead owner, but removes the corresponding log only if the directory is over budget. Unverifiable owners or malformed markers may conservatively retain disk space rather than risking deletion; this is residual storage, not a deadlock.
- Existing unmarked logs are eligible only if their tail contains a complete `worker_transcript_end` with `complete: true`; an older writer's size-limit marker does not prove execution has finished. Unmarked incomplete legacy logs and malformed/uncertain leases are conservatively protected rather than risking deletion of an older writer's running task. They may require manual cleanup once all Workers have stopped.
- The budget is **soft**, not a synchronous disk quota: active/protected logs can leave it over budget, and newly written bytes are checked only at the next eligible lifecycle check. If no later task starts/finishes there is no background sweep. A crashed claimed scan retries in a later interval. IO errors skip cleanup without failing/cancelling a task. Unknown files, nonregular files, and symlinks are never removed or followed.

Tests: `transcript.test.ts`, `transcript-retention.test.ts` and local child fixtures exercise the production stdout pipeline and retention without model/network calls or huge files: empty fork headers with unchanged fork execution, real task events, permissions, active processes, stale crashes, PID reuse, age order, exact-budget retention, cross-process throttling/concurrency, symlinks, and failure tolerance.
