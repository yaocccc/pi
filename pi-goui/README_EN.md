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

Exclusive persistent sessions require Unix file locking; use WSL on Windows. Platforms without file-lock support refuse persistent-session writes and can use `--no-session` for temporary interaction within one instance.

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

The terminal fills the page, with floating controls for connection status, reconnect, copy/paste, restart, instance selection and logout. Layout, tabs, slash commands, tool details, questionnaires and Worker panels reuse the original TUI. Browsers and a desktop connected to one instance share its input, output, draft, cursor, session selection and scrolling; concurrent typing may interleave. A separately created TUI follows the exclusive-session rules below, without sharing another TUI’s live execution state. Use slash commands when the browser reserves a shortcut.

Closing a page, logging out or disconnecting does not stop its TUI or task. Reconnection restores that same instance, including drafts and pending dialogs. The server reclaims dead browser connections only. Returning to the instance list disconnects the page; closing, restarting or quitting an instance exits its TUI and reclaims its own Pi, leaving independent instances alone. Read-only viewers do not take over the task. Stale pages cannot resurrect deleted instances. Stopping WebUI reclaims only TUIs it created and disconnects desktop proxies; discovered desktops continue independently. Terminal replay is bounded; Pi owns the full history.

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

When upgrading from the shared-executor version, let tasks finish and exit every old TUI and Pi using the affected sessions. The new version does not migrate running executors. It refuses acquisition when old writers remain or their exit cannot be confirmed. Do not mix old goui or native Pi writes with the new protocol, or force recovery by deleting lock files.

Login URLs and QR codes appear **only in the separate WebUI**. Desktop `--listen` / `--token` are no longer accepted. No tmux is required. Desktops started by older versions or with `--share=false` cannot be attached retroactively; restart with the current default behavior.

- Press **Ctrl+]** on the desktop to detach its display without stopping tasks; Enter reattaches. No QR is printed.
- The desktop and multiple browser clients can view and operate the same instance simultaneously, with no takeover popup. Opening/closing a page does not stop the instance. Explicit restart, close-instance and `/quit` actions affect every connected client of that instance.
- **One TUI mirrors working statistics**: its desktop and attached browsers show the same turns, tokens, TPS and clock. A separate TUI reads persisted history and ownership only; it does not subscribe to live statistics, streaming output or pending questionnaires.
- **The most recently viewing/active client determines the shared grid**: a new browser connection uses its own dimensions; subsequent keyboard, mouse or touch activity switches to that client's dimensions. All clients follow the same grid; smaller browser screens can scroll overflow. Passive window changes update that client's preferred size, but only the active client's changes immediately reflow the TUI. Output, terminal replies and heartbeats never claim geometry. When the active client leaves, the previous online active client resumes sizing. Browser-only instances use the same rule.
- The desktop remains attached to its original shared instance; independent browser instances do not change its directory. If the browser explicitly closes the desktop instance, pressing Enter on the desktop reopens the original launch directory.
- The desktop host belongs to its foreground process, not a daemon. Exiting reclaims its own TUI and Pi, leaving separate WebUI instances alone. Read-only TUIs do not continue that task. Ctrl+C in the detached prompt or SIGTERM stops the host; `/quit` also exits it while attached. `--check` neither registers an instance nor starts a listener.

## Usage and configuration

Switching tabs preserves drafts, queues, dialogs and tasks; reopening a session in one TUI focuses its existing tab. Opening, focusing and browsing a saved session reads disk without starting Pi or acquiring execution ownership. An explicit write first acquires the lock, then starts a fresh Pi and loads current history. Blank tabs also remain local until a write. Explicit `--no-session` has no reloadable history, so that mode starts and retains its local Pi until the tab closes and does not participate in persistent-session handoff.

Resuming history parses native JSONL in the background. Sidebar candidates, hovered history and resume-list selections receive bounded local preloading, with file identity, size and mtime validation on cache hits and no Pi startup. The first explicit submission retains its input and attachments, acquires the lock, starts Pi, loads authoritative state, validates history and submits exactly once. Later drafts remain separate; repeated Enter while connecting cannot replace the first intent. Real connection failures preserve readable history and unsent content, and unknown outcomes are never automatically replayed.

### Terminal mirroring versus independent session viewers

- Desktop and browser clients attached to **one TUI** mirror its whole terminal. Among **independent TUIs** opening one session, only the writer holds the session lock. Others show read-only status and the holder PID without a popup, refreshing persisted foreground history about every second. Drafts, attachments, scrolling, tabs and layout remain local. Read-only views do not forward sends, stops, queues, model changes or extension actions.
- Tools, questionnaires, queues and background workers belong to the executing instance. Main-run `agent_settled` alone does not release ownership. After native activity, pending interactions, compaction, queues and observable extension work finish, the TUI closes Pi, confirms writer exit and releases the lock. Other instances become interactive without submitting their drafts. The next manual write acquires ownership and loads current history again.
- There is no election, persistent cross-TUI executor, event broadcast or snapshot replay queue. Normal idle keeps the tab open and releases Pi; any instance may execute the next turn. SIGSTOP never allows timeout-based takeover, and a surviving Pi remains protected if its parent TUI dies. Crashes do not promise lossless takeover.
- Rename, delete and other direct history mutations use the same session lock and refuse changes while busy. OS file locks coordinate participating programs only; native `pi`, older goui and other editors can bypass this protocol and must not write concurrently.
- Locks and diagnostic records live in `~/.pi/goui-locks/`, outside agent directories. The same real session file is exclusive even across different agent configurations. Lock inodes are never deleted or replaced; normal release removes active records. After a crash, OS `flock` remains the final write authority: a stale Claim is removed automatically only while holding the original lock and after confirming the old owner, Pi and its process group have exited. PIDs are checked with birth identities; a missing PID alone never permits takeover. Unconfirmed startup identity, failed process checks or recorded cleanup failures remain read-only. Restored permission does not imply a successful prior operation or complete history, and never replays inputs. Unknown writer lifecycle, legacy compatibility or extension activity retains read-only ownership with a reason, without silent restart or retry. Confirm old instances and Pi processes have exited before upgrading; legacy directories are never migrated or force-deleted automatically.

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
- [internal/runtime/](internal/runtime/): direct RPC, process cleanup, session file locks and desktop discovery; no core or extensions dependency.
- [internal/extensions/](internal/extensions/): third-party protocols, state, UI and rendering; exports an interface slice for core to compose.

Only `core/`, `extensions/` and `runtime/` belong directly under `internal/`. See the [extension guide](internal/extensions/README_EN.md) for integration design and [AGENTS_EN.md](AGENTS_EN.md) for development constraints.

## Responsiveness and replay limits

Worker history parsing and event replay run on an exclusively owned background copy, committed only to the originating viewer. Malformed history or reset records preserve readable content and report an incomplete recording; a later completion cannot hide the error. Web terminal replay retains the latest 4 MiB and at most 16 size segments, with at most 8 MiB of reusable backing storage excluding connection send queues. A sliding window avoids moving the whole history for every output chunk. Replay is not a session archive and does not truncate native records.

Direct RPC bounds queued bytes and event count, pausing reads to apply backpressure when the consumer falls behind. Legal large events are delivered whole without a shared-broadcast budget terminating the session. Responses and events still follow Pi’s JSONL order: consumers must keep draining events rather than requiring later responses to bypass unlimited paused output.

## Verification

Commands and isolation requirements are in [AGENTS_EN.md](AGENTS_EN.md#verification). Check the fake-RPC fixture itself with `PYTHONDONTWRITEBYTECODE=1 python3 tests/test_fake_rpc.py`; complete companion-extension regression is documented in the [extension overview](../extensions/README_EN.md#update-uninstall-and-maintain). Tests default to fake RPC; optional real-RPC checks must use the read-only allowlist proxy, never model/tool calls. Passing tests does not establish real-terminal or cross-platform acceptance.

On Linux, `python3 script/test-resume.py` covers disk-only previews, first-write connection barriers, blank-session resume, independent drafts and connection failures, plus cold and prewarmed first paint for roughly 17 MB of history. `python3 script/test-session-ownership.py` exercises lazy startup, read-only contention, idle handoff, latest-history reload, SIGSTOP protection, surviving writers after parent death, automatic recovery after Pi exit and simultaneous takeover using real independent TUIs. `python3 script/test-shared-web.py` covers HTTP/WS startup, independent session handoff, disconnection survival and one-TUI mirroring. These scripts isolate HOME/agent/cwd and use fake RPC only; they do not establish real-model, physical-phone or other-platform acceptance.

WebUI also has dependency-free JS tests; set `CHROME_BIN` to enable real headless Chromium terminal interaction tests (mock WebSocket; no model calls):

```sh
node --test internal/core/web/assets/*.test.mjs
```

## Credits

Structure and RPC lifecycle draw on [cavaldos/pitago](https://github.com/cavaldos/pitago). Its MIT notice is retained in [LICENSE](LICENSE). The browser terminal embeds xterm.js 6.0.0, addon-fit 0.11.0 and addon-unicode-graphemes 0.4.0; MIT notices and width-synchronization notes are retained in `internal/core/web/assets/vendor/`. Font provenance and OFL/Apache notices live in `internal/core/web/assets/fonts/`.
