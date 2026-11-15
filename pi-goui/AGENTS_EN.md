# Pi Go UI development guide

[简体中文](AGENTS.md)

Applies to this directory together with parent `AGENTS.md` instructions. Inspect the worktree, preserve unrelated changes, and do not commit unless asked. See [README](README_EN.md) for usage and the [extension guide](internal/extensions/README_EN.md) for integration design and rules.

## Change constraints

- Keep distribution independent of author paths, personal configuration and adjacent TypeScript sources. Never silently fall back from an explicitly selected Pi executable. Startup must not copy credentials, inject extensions or rewrite original settings.
- Only `core/`, `extensions/` and `runtime/` belong under `internal/`. Native Pi tools, codemode and generic interaction/projection belong to core; third-party implementations and focused tests belong to extensions. RPC, process lifecycle, session file locks and desktop discovery belong to runtime, which must not depend on core or extensions. Follow the extension guide's single entry point, interface injection and dependency direction; do not add compatibility forwarding layers or production APIs solely for tests.
- Each writable session has at most one lock holder. Independent TUIs have no election, shared executor or live business-state broadcast. Acquire the lock for an explicit write, then start Pi and load current history. Browsing/focusing reads disk only. If another instance holds the session, show read-only status and its owner without a popup, periodically refresh persisted history, and preserve local drafts, attachments, tabs and layout. Async work captures its originating backend, execution cycle, tab/inspector, page and revision; reject stale replies and return goroutine results through the event loop.
- Switching/resuming preserves drafts, queued input, tasks and pending dialogs; discarding requires confirmation. Rename/delete remain bound to the selected target and briefly acquire the same session lock, validating paths, format and file identity and rejecting symlinks. After tasks, queues, pending interactions, workers and deferred writes finish, close Pi and confirm writer exit before releasing the lock. Normal handoff leaves the tab usable rather than displaying a crash. Exit cleans up owned children, listeners and waiters.
- Keep locks and diagnostic metadata in private user runtime directories, outside the real agent directory; never infer the agent from lock ancestry. Detect old shared executors and legacy `<agent>/goui-rpc`, failing closed if old writers cannot be ruled out. Do not migrate or force-delete them. Tests use isolated HOME directories.
- OS locks establish ownership: never take over a SIGSTOP process on heartbeat timeout or unlink/replace a lock inode. The actual Pi writer retains lock protection if its parent TUI dies. Recover stale Claims only while holding the original flock, validating process birth identities and ruling out the old writer/process group. Reuse the registry lock to serialize recovery and acquisition, recheck the Claim/token, and never delete a new owner's record. Unknown startup/writer ownership fails closed; never start another writer or automatically replay an operation with an unknown result. Idle restores interaction in other instances without submitting their drafts. Browser disconnection is not exit of its TUI.
- Disk previews during resume are local, read-only projections: load them in the background and validate tab, path, revision and backend before applying. Sending requires an authoritative snapshot. Speculative preloading must be bounded and deduplicated, never start a backend, and validate file identity, size and mtime on cache hits rather than trusting the path alone. Retain only the explicit first submission intent while connecting, preserve subsequent drafts, and never replay unknown outcomes. Real connection failures preserve history and remain explicit.
- Only the executing TUI accumulates working statistics. Its desktop/Web mirrors share those statistics naturally; independent read-only TUIs do not reconstruct live statistics. `agent_end` does not freeze the clock; `agent_settled` does, and the next run resets it. Runtime must not interpret third-party payloads; the single composition entry injects independently owned per-run capability usage.
- `agent_settled` ends the main run but alone does not prove the lock can be released. Confirm native idle, empty queues, no pending interactions and extension inactivity before closing the backend. Unknown activity retains ownership with an explicit reason. Prefer `get_entries` for the full active branch; compaction must not replace original records, and failed history refreshes must not corrupt existing state.
- Parse heavy history and replay batches on exclusively owned background copies. Copying a `State` struct does not isolate its slices, maps or extension capabilities; validate the originating viewer and job generation on commit. Statistics/command metadata must not unnecessarily invalidate transcript caches or in-flight layouts; malformed resets preserve readable content. Bound Web replay bytes and size segments without moving the entire history for every output chunk.
- Render presentation copies only: do not read current files to invent diffs/history or mutate capability state. Preserve unknown fields, real blank lines and full details; preview limits must not truncate stored data or details. Missing, failed or partial records must not appear successful.
- Treat backend text as untrusted; reuse sanitization and ANSI/Unicode-safe drawing. Keep layout and hit geometry aligned and safe at narrow widths; exclude image bytes from summaries. Caches account for content, width, folding, provider identity and inspector version; freeze display during dragging/selection and refresh afterward.
- User remapping and empty-array disabling override component default keys. Preserve the real terminal cursor and IME positioning. Dialogs and inspectors handle Esc first; otherwise, aborting active/pending prompts takes priority over completion.
- Execution, permissions, scheduling, authoritative validation and private storage belong to backends. Do not infer business settings from model names or access third-party private files. Generic session browsing, clipboard, images, Git, quota and frontend preferences may use local I/O; quota requests still require credential isolation, restricted destinations and timeouts.
- Maintain only three bilingual entry points: root README for usage, AGENTS for change constraints, and `internal/extensions/README` for integration. Chinese is default, English uses `_EN.md`, with top-of-file cross-links. Avoid new package guides, duplicate designs, pixel-level specifications and historical verification logs; link source for type details.

## Verification

Run inside `pi-goui/`. Prefer affected-package tests while iterating; for cross-module changes run:

```sh
go test ./...
go vet ./...
go build ./...
GORACE=atexit_sleep_ms=0 go test -race ./... -timeout=90s
git diff --check
```

Run `gofmt` on changed Go files. Keep protocol/formatting/lifecycle tests beside implementations and genuine host integration tests in core. Cover absent capabilities, unknown protocols, failure/cancellation, stale responses, cross-tab isolation and generic fallback; do not drop assertions to accommodate implementations.

For interaction changes, run `python3 script/test-ui.py` (temporary binary, isolated HOME, fake RPC). Optional `python3 script/test-ui.py --real-only --real-pi "$(command -v pi)"` uses [tests/real_rpc_guard.py](tests/real_rpc_guard.py) to allow only read-only queries, never model/tool operations.

Check the fixture itself with `PYTHONDONTWRITEBYTECODE=1 python3 tests/test_fake_rpc.py`. Performance regression should use synthetic large histories, isolated preparation/commit stages and bounded-work assertions—not tiny histories or hidden loading labels. Optional benchmarks: `go test ./internal/core/web -run '^$' -bench '^BenchmarkTerminalHistoryAppend$'` and `go test ./internal/extensions/hostui -run '^$' -bench '^BenchmarkWorkerPreparedReplayStages$'`; these do not establish end-to-end latency.

For slow-connection history previews and deferred submission, run `python3 script/test-resume.py`. For Linux lifecycle acceptance, run `python3 script/test-session-ownership.py` and `python3 script/test-shared-web.py`: temporary builds and isolated HOME/agent/cwd exercise lazy startup, read-only contention, idle handoff, draft preservation and Web disconnection survival using real PTYs/HTTP/WS with fake RPC, never model calls.

For portability changes, check the binary with `--check --no-session` outside the checkout under an isolated HOME and `PI_CODING_AGENT_DIR`; do not copy personal settings/models/auth. Changes to companion TypeScript follow its own development and verification guides.

Report checks actually run, failures and unverified areas. Unit tests do not establish terminal, model or cross-platform acceptance; historical baseline failures are not passes. Documentation changes require bilingual consistency, local-link and removed-reference checks, not a full application test run.
