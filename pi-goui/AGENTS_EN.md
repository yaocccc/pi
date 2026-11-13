# Pi Go UI development guide

[简体中文](AGENTS.md)

Applies to this directory together with parent `AGENTS.md` instructions. Inspect the worktree, preserve unrelated changes, and do not commit unless asked. See [README](README_EN.md) for usage and the [extension guide](internal/extensions/README_EN.md) for integration design and rules.

## Change constraints

- Keep distribution independent of author paths, personal configuration and adjacent TypeScript sources. Never silently fall back from an explicitly selected Pi executable. Startup must not copy credentials, inject extensions or rewrite original settings.
- Only `core/` and `extensions/` belong under `internal/`. Native Pi tools, codemode and generic host behavior belong to core; third-party implementations and focused tests belong to extensions. Follow the extension guide's single entry point, interface injection and dependency direction; do not add compatibility forwarding layers or production APIs solely for tests.
- Each writable session has one backend process. Async work captures its originating backend, tab/inspector, page and revision; reject stale replies rather than routing to the currently active tab. Goroutines return results through the event loop, never mutate UI state directly.
- Switching/resuming preserves drafts, queued input, tasks and pending dialogs; discarding requires confirmation. Rename/delete remain bound to the selected target and exclude concurrent in-app mutations. Direct native-history operations validate paths, format and file identity, rejecting symlinks. Exit cleans up children, listeners and waiters.
- Busy ends on `agent_settled`, not prompt acknowledgement. Prefer `get_entries` for the full active branch; compaction must not replace original records, and message indices must not count screen rows. Failed history refreshes must not corrupt existing state.
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

For portability changes, check the binary with `--check --no-session` outside the checkout under an isolated HOME and `PI_CODING_AGENT_DIR`; do not copy personal settings/models/auth. Changes to companion TypeScript follow its own development and verification guides.

Report checks actually run, failures and unverified areas. Unit tests do not establish terminal, model or cross-platform acceptance; historical baseline failures are not passes. Documentation changes require bilingual consistency, local-link and removed-reference checks, not a full application test run.
