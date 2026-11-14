# Pi Go UI development guide

[简体中文](AGENTS.md)

Applies to this directory together with parent `AGENTS.md` instructions. Inspect the worktree, preserve unrelated changes, and do not commit unless asked. See [README](README_EN.md) for usage and the [extension guide](internal/extensions/README_EN.md) for integration design and rules.

## Change constraints

- Keep distribution independent of author paths, personal configuration and adjacent TypeScript sources. Never silently fall back from an explicitly selected Pi executable. Startup must not copy credentials, inject extensions or rewrite original settings.
- Only `core/`, `extensions/` and `runtime/` belong under `internal/`. Native Pi tools, codemode and generic interaction/projection belong to core; third-party implementations and focused tests belong to extensions. RPC, cross-process identity, elections, stable session executors and desktop discovery belong to runtime, which must not depend on core or extensions. Follow the extension guide's single entry point, interface injection and dependency direction; do not add compatibility forwarding layers or production APIs solely for tests.
- Each writable session has one stable executor/Pi within its user/canonical-agent domain. Actual TUIs elect a coordinator; outer desktop hosts, Web servers and helpers do not participate, and elections must not restart Pi. Share business state, but retain each independent TUI's drafts, attachments, tabs and layout locally. Async work captures its originating backend, tab/inspector, page and revision; reject stale replies rather than routing to the currently active tab. Goroutines return results through the event loop, never mutate UI state directly.
- Switching/resuming preserves drafts, queued input, tasks and pending dialogs; discarding requires confirmation. Rename/delete remain bound to the selected target and exclude concurrent in-app mutations. Direct native-history operations validate paths, format and file identity, rejecting symlinks. Exit cleans up owned children, listeners and waiters; shared executors are reclaimed only after the last client leaves and no file-mutation lease remains.
- Shared RPC registrations live only in `~/.pi/goui-rpc/<canonical-agent SHA-256>/`. Native backends still use the real agent directory; never infer it from registry ancestry. Detect an existing legacy `<agent>/goui-rpc` read-only and fail closed without migration, deletion or rewriting. Tests use isolated HOME directories inherited unchanged by helpers.
- OS locks establish ownership: never take over a SIGSTOP member on heartbeat timeout or unlink/replace a lock inode. Unknown startup/writer ownership fails closed; never fall back to a second writer or automatically replay an operation with an unknown result. Executor crashes do not promise lossless takeover; file mutations require an exclusive lease, and browser disconnection is not TUI exit.
- Disk previews during resume are local, read-only projections: load them in the background and validate tab, path, revision and backend before applying. Sending requires an authoritative snapshot. Speculative preloading must be bounded and deduplicated, never start a backend, and validate file identity, size and mtime on cache hits rather than trusting the path alone. Retain only the explicit first submission intent while connecting, preserve subsequent drafts, and never replay unknown outcomes. Real connection failures preserve history and remain explicit.
- The stable executor maintains working accumulation through the shared algorithm. Late joiners receive bounded, publication-ordered projections with original timing and capability usage—not business replay or full-history reparsing. UI rejects stale results by originating client and monotonic revision and only formats/dismisses statistics locally. `agent_end` does not freeze the clock; `agent_settled` does, and the next run resets it. Runtime must not interpret third-party payloads; the single composition entry injects independently owned per-run capability usage.
- Busy ends on `agent_settled`, not prompt acknowledgement. Prefer `get_entries` for the full active branch; compaction must not replace original records, and message indices must not count screen rows. Failed history refreshes must not corrupt existing state.
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

For slow-connection history previews and deferred submission, run `python3 script/test-resume.py`. For Linux shared-lifecycle acceptance, run `python3 script/test-shared-session.py` and `python3 script/test-shared-web.py`: temporary builds and isolated HOME/agent/cwd exercise one backend, elections, extension interactions and cleanup using real PTYs/HTTP/WS with fake RPC, never model calls.

For portability changes, check the binary with `--check --no-session` outside the checkout under an isolated HOME and `PI_CODING_AGENT_DIR`; do not copy personal settings/models/auth. Changes to companion TypeScript follow its own development and verification guides.

Report checks actually run, failures and unverified areas. Unit tests do not establish terminal, model or cross-platform acceptance; historical baseline failures are not passes. Documentation changes require bilingual consistency, local-link and removed-reference checks, not a full application test run.
