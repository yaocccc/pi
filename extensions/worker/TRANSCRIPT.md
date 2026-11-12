# Worker transcript v1 (read-only viewer protocol)

The Worker CLI/session isolation is unchanged: `new` still uses `--no-session`; `fork` still executes against a temporary captured branch which is deleted after exit. The transcript is a **separate append-only event log**, not a native Pi session and must never be passed to `--session`, resumed, or opened by a second writing agent.

## Discovery and lifetime

- Root: `path.join(path.dirname(agentDir()), "worker-transcripts")`, normally `~/.pi/worker-transcripts` (beside `~/.pi/agent`, not inside it). `agentDir()` is the existing Worker configuration resolver: `PI_CODING_AGENT_DIR` when set, otherwise SDK `getAgentDir()` (path-resolved). When upgrading from agent-local storage, move the old `agentDir()/worker-transcripts` directory here while Workers are stopped, then reload the extension and restart the Go UI; UUID descriptors remain unchanged.
- Task field: `transcript: { version: 1, id: "<UUID>" }`. This is published in `ChildProgress`, returned in `ChildResult`/host task result, copied into `WorkerUiTask`, and retained in `worker-snapshot-v1` parent history. An optional `incomplete: true, error: "size_limit" | "io_error" | "protocol_error"` flags known recording failure; the descriptor never contains a filesystem path.
- Resolve **only a validated UUID** to `<root>/<id>.jsonl`; reject unknown versions, path traversal, symlink files and symlink escape. Do not accept an arbitrary path from task text/results. Use the host-produced task descriptor rather than a model-returned value.
- Files are created exclusively with random UUID names, mode `0600`; root mode `0700`. Existing symlink roots are refused. Permissions are private, not encryption: real task messages/tool results may contain sensitive data.
- Logs survive task completion/cancellation and parent session switches. Old tasks without a descriptor have no recoverable record. There is no automatic retention cleanup in v1. Users may delete obsolete logs from this dedicated directory when no longer needed; viewers must tolerate missing files.

## JSONL framing and replay

UTF-8 records delimited by **LF**, not Unicode U+2028/U+2029. Readers must retain a partial last line while a task is running; do not rescan the entire log on each frame. Native JSON stdout chunks are decoded with `StringDecoder`, so split multibyte characters survive.

First record:

```json
{"type":"worker_transcript_header","version":1,"id":"12345678-1234-4234-8234-123456789abc","cwd":"/project","entries":[]}
```

`entries` is empty for `new`. For `fork` it contains the admission-time active branch entries captured by `getBranch()`, in original order (not inactive branches and not the parent's subsequently mutated manager). Initialize the viewer with these entries using its normal entry projection: the header itself is a valid `State.LoadEntries` envelope because it contains `entries` (no `leafId` means its last entry). Set cwd from the header separately; then send subsequent session-event lines to `State.Apply`. System-message entries and compaction `systemMessage` checkpoints are omitted to avoid duplicating hidden system prompts/tool declarations. Parent links crossing an omitted entry are rewired to the preceding visible entry, and compaction first-kept references are adjusted. Remaining entries retain their IDs and content; this is display history, not a resumable model context.

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

Each task log is capped at 64 MiB, with a small terminal-marker reserve. Once the cap is reached, recording stops and both descriptor and terminal marker explicitly report `size_limit`; a too-large inherited branch writes an empty header plus that marker rather than pretending to be a complete new session. No hidden truncation is presented as full history.

Synchronous bounded appends preserve event order and avoid an unbounded in-memory promise/write queue. The file is visible immediately to a read-only incremental viewer; normal completion fsyncs then closes it. IO errors never fail/cancel the Worker itself. On disk failure a final marker may be impossible, so the descriptor carries the error and readers must treat a missing/partial tail conservatively. Hard process death cannot guarantee a final marker.

Tests: `transcript.test.ts` plus the local `fixtures/transcript-child.mjs` exercise the production stdout pipeline without model/network calls, including split UTF-8, live access, new/fork, failure/cancellation, permissions, limits and parent-history persistence.
