# pi-goui

[简体中文](README.md)

An independent Go frontend with a Bubble Tea TUI and minimal browser WebUI, using standard Pi RPC for models, tools, authentication and extensions. Copy the entire `pi-goui/` directory to build, or distribute the binary alone; no other checkout directories or author-specific settings are required at runtime.

## Build and run

Building requires Go 1.26+; running requires an installed, authenticated Pi / Node.js environment. From this directory:

```sh
go build -trimpath -o bin/pi-goui ./cmd/pi-goui
./bin/pi-goui --cwd /path/to/project
./bin/pi-goui --check
```

`./run.sh` builds and starts the desktop. On Unix it automatically provides a private local synchronization endpoint and **immediately opens the TUI: no QR code and no TCP web listener**. Start WebUI separately, in either order. Use `--share=false` to disable discovery/synchronization; unsupported platforms retain ordinary desktop mode. Common options:

| Option | Purpose |
| --- | --- |
| `--pi /path/to/pi` / `PI_BIN` | Select the Pi executable; otherwise use PATH |
| `PI_CODING_AGENT_DIR` | Select the agent directory; defaults to `~/.pi/agent` |
| `--continue` / `-c`, `--session file.jsonl` | Resume the latest session in this cwd, or open a specific session |
| `--no-session` | Disable persistence; cannot be combined with resume options |
| `--check` | Check RPC startup without sending a prompt or saving a session |
| `--share=false` | Disable automatic desktop discovery/synchronization; enabled by default, no `--share` flag needed |
| `-- --model provider/model --no-extensions` | Forward Pi options; cannot override RPC mode/session ownership or pass positional prompts |

Authenticate in native `pi`; do not copy someone else's credentials or settings. Starting the frontend does not automatically install extensions or invoke a model. **Submitted prompts retain Pi's normal permissions and may incur costs; this is not a sandbox.**

## Browser and LAN WebUI

The separate `cmd/webui` entry point runs the same Bubble Tea TUI in a real PTY, forwarding input, output and dimensions to xterm.js over WebSocket—not a separate chat interface. The TUI and browser assets are embedded in one binary: no separate `pi-goui` installation, npm build or CDN is required. The Web terminal supports Linux/macOS and other Unix systems, using the system `/bin/ps` to enumerate and reclaim independent backend process groups; use WSL on Windows. From this directory:

```sh
go run ./cmd/webui --cwd /path/to/project
# Or build a standalone binary
go build -trimpath -o bin/webui ./cmd/webui
./bin/webui --check
./bin/webui --listen :8888 --key 'my-lan-key' --cwd /path/to/project
```

The default listener is `127.0.0.1:8888` only. Each WebUI startup prints its login token (randomly generated when unspecified), a `?pk=token` login link and a terminal QR code. Scanning/opening the link logs in automatically; the page immediately removes `pk` from the address bar and never stores it in localStorage. Manual token login remains available. **Use explicit `--listen :8888` for phone access**: wildcard listeners advertise an active interface's private IPv4 and the actual port. The default loopback QR only works locally. With multiple interfaces, check that the advertised IP is reachable from your phone.

Use `--key 'my-lan-key'` for a fixed login token; the existing `--token` alias and `PI_WEBUI_TOKEN` environment variable remain supported; prefer the environment variable to avoid shell history. The token, login link and QR code are service credentials, not model API keys. Do not share them or publish screenshots; links can still remain in chat messages, proxy logs and similar locations.

| WebUI option | Purpose |
| --- | --- |
| `--listen host:port` | Listen address; defaults to loopback only |
| `--key value` / `--token value` | Fixed login token; falls back to `PI_WEBUI_TOKEN`, then random generation if empty |
| `--pi`, `--cwd`, `--agent-dir` | Pi executable, default project directory and agent directory; relative directory paths resolve against the launch directory |
| `--session file.jsonl` / `--no-session` | Initially resume a session or disable persistence; mutually exclusive |
| `--request-timeout 30s` | RPC request timeout for the read-only `--check` |
| `--tls-cert cert.pem --tls-key key.pem` | Enable HTTPS; certificate and key must be supplied together |
| `--check` | Disable persistence and query only `get_state` / `get_entries`, then exit; no model or tool execution |
| `-- --model provider/model --no-extensions` | Forward the same safe startup options as the TUI; RPC/session ownership cannot be overridden |

Login and refresh show the **instance picker** without automatically starting or connecting to a TUI. While this page is open, it refreshes automatically as desktops start or exit. Choose “桌面共享实例” to view/control the same TUI alongside the desktop, or connect to another existing instance. “新建实例” first opens the directory picker, then creates an **independent TUI** without stopping or stealing the desktop or other instances. An explicit `--session` applies only to the original default instance; new instances never reuse that file, even in the same directory. Standalone `webui --session …` offers a default-instance entry.

The directory list reads native session headers, combines them with directories selected during this service run, and shows at most **10 distinct existing directories**. It does not invent missing entries; without history it offers the launch `--cwd`. You can also enter another absolute server directory. Reading the list does not rewrite native history or Pi settings.

The page and terminal embed JetBrainsMono Nerd Font Mono 3.5.1 (regular, bold, italic and bold italic), with the host's WenQuanYi Micro Hei Mono as bundled Chinese fallback; browser devices need no font installation. All faces finish loading before terminal initialization. Go TUI wide-character/Emoji widths are synchronized to avoid font-swap or grapheme-induced frame misalignment. On touch devices, tapping messages, tools or the footer does not request the soft keyboard; tapping the terminal editor enables it. Single-finger vertical swipes inside messages, lists or inspectors become native TUI wheel events targeting the region where the touch started. Swipes neither activate options nor request the keyboard; normal taps still select items.

The terminal fills the page. A floating top-left icon expands connection status, reconnect, copy/paste, restart, “返回实例列表”, “关闭实例 / 换目录” and logout controls without reserving layout space; click outside or press Esc to close it. The page and terminal's blank areas share a dark blue-gray background. Layout, session tabs, slash commands, tool details, questionnaires and Worker panels are the original TUI, so the usage instructions below also apply in the browser. Each instance owns its TUI; independent TUIs opening the same session share its backend, as described below. Multiple browsers and the desktop can stay connected simultaneously, receive the same output, and send input to the same backend without disconnecting each other. Drafts, cursor, session selection and scrolling are shared too; concurrent typing may interleave, with no independent editing lock. Browser-reserved shortcuts may not reach the terminal; use the equivalent slash command instead.

Closing the page, logging out or disconnecting leaves tasks running. Background pages release their connection and reconnect to the same instance on foreground/network recovery. Server heartbeats reclaim dead connections without stopping the TUI. Tasks survive even when the OS kills the browser without an exit notification. When all browsers leave, the desktop remains usable: there is no ownership to return. After a refresh, reselect the current instance to resume it; automatic reconnect does not require reselecting. Drafts and pending dialogs stay intact. “返回实例列表” only disconnects this page and leaves tasks running. “关闭实例 / 换目录” exits the selected TUI and returns to the new-instance directory picker without affecting other instances; session backends still used by other independent TUIs continue running. Stale pages cannot resurrect deleted instances. Output replay is bounded, not a permanent terminal recording; Pi still owns the full session history. `/quit` ends the TUI without automatically restarting it; use “重启 TUI” to start again explicitly. That button exits the old TUI and releases its session connections. Stopping WebUI reclaims only its own independent TUIs and disconnects proxies. Shared backends still used by other TUIs and discovered desktops keep running; desktops remain discoverable after WebUI restarts. Never let native Pi or older goui versions outside this sharing protocol write the same history file concurrently.

Copy/paste uses the floating menu or `Ctrl+Shift+C/V`; TUI copy requests reach the browser clipboard only after an explicit user action. LAN HTTP commonly lacks the Clipboard API, so the page provides copy fallback or a manual paste dialog. Image paths, external editors and file-open operations still refer to the server, not the browser's local desktop.

**A LAN is not automatically trusted: authenticated users can run Pi tools with the host process's permissions. This service is neither a sandbox nor a multi-user permission isolation system.** Default HTTP does not encrypt tokens, messages or tool output. Use HTTPS or a trusted secure tunnel on untrusted networks; do not expose the service directly to the public Internet. Startup does not install extensions, copy credentials or modify Pi settings. Stop with `Ctrl+C` / `SIGTERM`.

### Desktop and multiple browsers sharing one instance

Run these in separate terminals, **in either order**:

```sh
# Desktop: opens the TUI immediately, without a QR code
./run.sh --cwd /path/to/project
# Separate WebUI: explicitly enable LAN access for phones
./bin/webui --listen :8888
```

WebUI discovers goui desktops running as the **same OS user with the same agent directory**. Multiple desktops appear separately; selection connects without replacing existing clients. Each desktop registers a private Unix socket (directory `0700`, descriptor/socket `0600`), uses no TCP port, and does not depend on WebUI being alive. The registry lives in `~/.pi/goui-instances/`, outside the agent directory; each descriptor records its agent directory to preserve discovery isolation; it neither copies model credentials nor changes native Pi settings. Normal exit removes its own registration; offline remnants are not listed as available. Different agent directories are isolated: match desktop `PI_CODING_AGENT_DIR` with WebUI `--agent-dir`.

Existing shared executors do not hot-upgrade when binaries are rebuilt, windows open or pages refresh, and cannot reconstruct current-run statistics they never recorded. For upgrades, let the task finish, normally exit all old instances using that session, then let the new version create its executor. Do not interrupt an active task merely to display statistics.

Login URLs and QR codes appear **only in the separate WebUI**. Desktop `--listen` / `--token` are no longer accepted. No tmux is required. Desktops started by older versions or with `--share=false` cannot be attached retroactively; restart with the current default behavior.

- Press **Ctrl+]** on the desktop to detach its display without stopping tasks; Enter reattaches. No QR is printed.
- The desktop and multiple browser clients can view and operate the same instance simultaneously, with no takeover popup. Opening/closing a page does not stop the instance. Explicit restart, close-instance and `/quit` actions affect every connected client of that instance.
- **Independent instances share working statistics**: opening the same native session mid-run gives a new TUI/browser instance that run's turns, input/output tokens (including configured capability usage), TPS and original start time—not just `working` or a restarted clock. Statistics freeze at settlement and reset for the next run; local drafts and display preferences remain independent.
- **The most recently viewing/active client determines the shared grid**: a new browser connection uses its own dimensions; subsequent keyboard, mouse or touch activity switches to that client's dimensions. All clients follow the same grid; smaller browser screens can scroll overflow. Passive window changes update that client's preferred size, but only the active client's changes immediately reflow the TUI. Output, terminal replies and heartbeats never claim geometry. When the active client leaves, the previous online active client resumes sizing. Browser-only instances use the same rule.
- The desktop remains attached to its original shared instance; independent browser instances do not change its directory. If the browser explicitly closes the desktop instance, pressing Enter on the desktop reopens the original launch directory.
- The desktop host belongs to its foreground process, not a daemon. Closing it releases its TUI connections, not WebUI or its independent instances. A session backend continues while another independent TUI uses it and is reclaimed after the last TUI leaves. Ctrl+C in the detached prompt or SIGTERM stops the desktop host. `/quit` also exits it while the desktop is connected to the TUI. `--check` remains read-only and neither registers an instance nor starts a listener.

## Usage and configuration

Switching tabs preserves drafts, queues, dialogs and running tasks; reopening a session in one TUI focuses its existing tab. On Unix, independent goui instances (including Web-spawned TUIs) under the same user and canonical agent directory reuse one stable session executor and Pi process when opening the same native session. Different sessions remain independent. Non-Unix platforms retain per-TUI backends without this cross-process sharing.

Resuming history reads native records in the background for immediate browsing while connecting the backend, rather than withholding the transcript until Pi starts. Sidebar candidates, hovered history and the current resume-list selection receive bounded local preloading. A valid hit avoids reparsing without starting Pi early; file changes invalidate it. Cold previews parse JSONL directly instead of encoding and reparsing a large RPC envelope, though first opens still have real I/O and parsing costs. The initial sharing handshake does not fetch the full history twice. Statistics and command lists arrive after the authoritative body without blocking it or invalidating an already published or in-flight transcript layout. Unchanged snapshot parsing and history-file validation reuse cached results; changed files or session content are checked again. Editing remains available during connection. The first Enter retains the original input and attachments and submits them once after an authoritative snapshot and any required history validation; subsequent drafts stay separate. Additional Enter presses while that submission is pending do not replace it. Real connection failures remain explicit and preserve history and unsent content instead of a generic `stopped` label; operations with unknown outcomes are never automatically replayed.

### Terminal mirroring versus independent session sharing

- Desktop and browser clients attached to **one TUI** mirror its draft, cursor, tabs and dimensions. **Independent TUIs** opening the same session share messages, run state, model/thinking settings, queues, session name and extension business state, while keeping drafts, attachments, tabs and layout local. Sending, queueing or stopping from any client affects the shared session.
- Questionnaires are broadcast; the first answer successfully written to the backend closes them on all clients, while failed writes remain retryable. Late joiners receive pending interactions and persistent status/widget/title state; editor fills are not replayed as persistent shared state. Queue merging and submission are serialized by the executor to prevent interleaved queue clearing.
- Actual TUIs elect a coordinator; outer desktop hosts, pure Web servers and executors do not participate. Other TUIs elect a successor after coordinator exit or kill. Clients connect directly to stable executors, so elections keep Pi's PID unchanged and do not replay operations. SIGSTOP never triggers lock takeover on timeout. Browser disconnection does not exit its hidden TUI; the backend is reclaimed after the last TUI leaves and no file-mutation lease remains.
- Direct file deletion or mutation is refused while another TUI uses that session; file operations require an exclusive lease. Do not concurrently write the session using native `pi`, older goui versions or other programs that do not participate in this protocol.
- Coordination metadata lives in `~/.pi/goui-rpc/<agent-directory-hash>/`, namespaced by the SHA-256 of the canonical agent directory. Different agents remain isolated and symlink aliases of the same agent share a namespace; no runtime state is created inside the agent directory. Unknown startup outcomes, missing ownership metadata or surviving old writers fail closed rather than starting a second writer. Unknown operations are never automatically replayed; local drafts remain for manual inspection/recovery. Executor crashes **do not promise lossless takeover**. Do not force recovery by deleting live registrations or lock files; first verify the relevant processes and session file. Upgrades do not migrate running processes. If the old `<agent-directory>/goui-rpc` still exists, the new version fails closed even when that directory is empty; it never moves, deletes or rewrites it automatically. Verify that all old instances and their Pi backends have exited, then handle the old directory manually before starting the new version. Do not mix old and new versions.

| Command | Purpose |
| --- | --- |
| `/new`, `/resume`, `/sessions` | Create or browse sessions |
| `/model`, `/thinking` | Select a model or reasoning level |
| `/fork`, `/origin`, `/tree` | Fork, inspect the source, or browse the session tree read-only |
| `/attach` | Attach an image |
| `/theme`, `/reload` | Select a frontend theme or reload frontend configuration |
| `/hotkeys` | Show effective bindings and their contexts |
| `/close`, `/delete`, `/quit` | Close the current session, permanently delete after confirmation, or exit all sessions |

- Defaults: `Enter` submits; `Shift+Enter` / `Ctrl+J` inserts a newline; `Esc` aborts; `Ctrl+N` creates a session; `Ctrl+↑/↓` switches tabs; `Ctrl+L` selects a model; `Ctrl+T` toggles thinking; `Ctrl+End` returns to the latest message.
- `Ctrl+C` clears input; press twice with empty input to exit. `Ctrl+D` with empty input closes the current session, confirming first if busy. Closing the last session exits the frontend.
- Click tools to expand previews; “查看 ↗” opens full details with independent scrolling, selection/copy and resizing. `Esc` closes details. Preview truncation never changes saved records; estimated tokens are not billing usage.
- In the session picker, `Ctrl+R` renames and `Ctrl+D` requests confirmed deletion. Hover a user-message header and click “从此轮fork” to create a separate session directly while preserving the original tab/task; `/tree` does not edit the tree.
- Configuration comes from agent `keybindings.json` / `settings.json` and project `.pi/settings.json`. Overrides replace default bindings; an empty array disables an action. Enhanced chords depend on terminal support.
- `/theme` previews themes; Enter saves and Esc restores. Preferences live in agent `goui.json`, not native Pi theme settings. Missing theme files use embedded defaults; `/reload` does not reload backend extensions.

This frontend does not promise native TUI feature parity. Use `pi` for authentication, full scoped-model management and native settings editing.

## Optional extensions

Without third-party extensions, native Pi tools, codemode and generic results still work. Existing adapters support questionnaires, Worker, Codex levels and SoL-Pi/Web results. Follow each backend extension's own installation guide; Go does not load TypeScript directly.

Worker transcripts are fetched read-only through a backend capability; closing the inspector does not stop tasks. Unavailable records fall back to existing task results; partial records remain visible and marked incomplete. Codex controls appear only when the bridge and capability are available. See the [extension guide](internal/extensions/README_EN.md).

## Updates and compatibility

Startup asynchronously checks the selected Pi installation and unpinned npm/Git packages declared in global configuration, read-only. It never installs/upgrades packages, invokes a model or writes Pi settings. No update, network failure or an incompatible installation stays quiet; silence does not guarantee everything is current.

Any nonempty `PI_OFFLINE` or forwarded `-- --offline` skips network checks. Local Pi release notes may still appear; displayed versions are tracked only in a separate frontend cache. `--check` checks RPC only; `run.sh` only builds the local frontend.

## Code layout

- [cmd/pi-goui/](cmd/pi-goui/): TUI startup; [cmd/webui/](cmd/webui/): WebUI startup.
- [internal/core/](internal/core/): generic configuration, session projection, input, rendering and UI; `extension/` owns integration contracts and `extensionapi/` low-level projection/drawing interfaces.
- [internal/runtime/](internal/runtime/): RPC transport, cross-process identity/coordinator elections, stable session executors, submission/file leases and desktop discovery; no core or extensions dependency.
- [internal/extensions/](internal/extensions/): third-party protocols, state, UI and rendering; exports an interface slice for core to compose.

Only `core/`, `extensions/` and `runtime/` belong directly under `internal/`. See the [extension guide](internal/extensions/README_EN.md) for integration design and [AGENTS_EN.md](AGENTS_EN.md) for development constraints.

## Responsiveness and replay limits

Worker history parsing and event replay run on an exclusively owned background copy, committed only to the originating viewer. Malformed history or reset records preserve readable content and report an incomplete recording; a later completion cannot hide the error. Web terminal replay retains the latest 4 MiB and at most 16 size segments, with at most 8 MiB of reusable backing storage excluding connection send queues. A sliding window avoids moving the whole history for every output chunk. Replay is not a session archive and does not truncate native records.

## Verification

Commands and isolation requirements are in [AGENTS_EN.md](AGENTS_EN.md#verification). Check the fake-RPC fixture itself with `PYTHONDONTWRITEBYTECODE=1 python3 tests/test_fake_rpc.py`; complete companion-extension regression is documented in the [extension overview](../extensions/README_EN.md#update-uninstall-and-maintain). Tests default to fake RPC; optional real-RPC checks must use the read-only allowlist proxy, never model/tool calls. Passing tests does not establish real-terminal or cross-platform acceptance.

On Linux, `python3 script/test-resume.py` covers slow-connection previews, deferred first submission, blank-session resume, independent tab drafts and connection failures, and measures cold first paint and prewarmed sidebar mouse-click first paint for roughly 17 MB of history. `python3 script/test-shared-session.py` exercises real independent TUIs, one backend, SIGSTOP/SIGKILL/graceful elections, isolated drafts, mid-run late-join working statistics/clocks, questionnaires and cleanup. `python3 script/test-shared-web.py` exercises full HTTP/WS startup, Web child TUI elections, working statistics when Web starts during a desktop run, disconnection survival and cross-entry shutdown isolation. These scripts build temporarily and isolate HOME/agent/cwd with fake RPC only; they do not establish real-model, physical-phone or other-platform acceptance.

WebUI also has dependency-free JS tests; set `CHROME_BIN` to enable real headless Chromium terminal interaction tests (mock WebSocket; no model calls):

```sh
node --test internal/core/web/assets/*.test.mjs
```

## Credits

Structure and RPC lifecycle draw on [cavaldos/pitago](https://github.com/cavaldos/pitago). Its MIT notice is retained in [LICENSE](LICENSE). The browser terminal embeds xterm.js 6.0.0, addon-fit 0.11.0 and addon-unicode-graphemes 0.4.0; MIT notices and width-synchronization notes are retained in `internal/core/web/assets/vendor/`. Font provenance and OFL/Apache notices live in `internal/core/web/assets/fonts/`.
