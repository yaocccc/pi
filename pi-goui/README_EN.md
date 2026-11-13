# pi-goui

[中文](README.md)

A standalone Go / Bubble Tea v2 frontend for Pi. Each open session uses its own `pi --mode rpc` subprocess. You do not need the author's agent directory, themes, credentials, or extensions.

## Build and run independently

Copy the complete `pi-goui/` directory anywhere, or distribute only the compiled binary. No sibling repository directories are required at runtime.

```bash
cd /path/to/pi-goui
go build -trimpath -o bin/pi-goui ./cmd/pi-goui
./bin/pi-goui --cwd /path/to/project
./bin/pi-goui --check

# Alternatively, build and run while retaining the caller's current directory:
/path/to/pi-goui/run.sh
/path/to/pi-goui/run.sh -c
/path/to/pi-goui/run.sh --session /path/to/session.jsonl
```

Building requires Go **1.26+**; running requires a working Pi installation. `--pi` / `PI_BIN` selects Pi, `PI_CODING_AGENT_DIR` selects its agent directory, and `--no-session` disables session persistence. Missing custom themes use embedded defaults. Authenticate through `/login` in native Pi; never copy someone else's credentials or replace your existing settings just to install this frontend.

`--check` performs a read-only RPC smoke check without submitting a model prompt. Normal prompts still invoke Pi's models/tools with their usual permissions and costs. Startup does not install extensions or modify Pi settings.

## Optional third-party extensions

The repository's Worker, ask-question, Codex Fast and SoL-Pi integrations are **optional third-party adapters**, not frontend prerequisites. See the [extension installation guide](../extensions/README_EN.md) and [adapter boundaries](internal/extensions/README_EN.md).

- No installed capability: ordinary Pi tools and standard RPC dialogs remain usable; there is no Codex tier control.
- Codex tiers are discovered, read and changed through Pi RPC. Go does not read or write `codex-fast.json`, migrate it, or choose provider request tiers.
- Worker cards consume structured results and stable `phaseCode` values, not Chinese status text. The read-only inspector obtains records through `worker.transcript`; Go does not read private Worker logs or assume their location. Missing capability/records fall back to the existing task result, never guessed activity-as-history.
- Questionnaire views submit choices to the backend, which owns validation, queuing and cancellation.
- SoL-Pi plans and context notices are display projections, not another compaction implementation.

The optional `pi-ui` backend adapter supplies a capability-discovered RPC bridge. Unknown protocols keep standard dialog/tool fallbacks. Go does not import TypeScript or load extension source files.

## UI and sessions

- Multiple sessions retain independent subprocesses, drafts, queues, scroll position, folds and pending dialogs. Switching focus does not stop background work. Restoring an already open session activates it rather than creating a second writer.
- The sidebar shows session/directory, context and usage, recent sessions, Git information and eligible Codex quota. Model/thinking controls are in the input footer. Narrow terminals and inspectors adjust the layout.
- New empty sessions support directory selection; input supports multiline editing, Unicode-aware movement, fuzzy slash commands, bounded asynchronous `@file` completion, clipboard integration and external editors.
- Messages use Markdown/code rendering and expandable tool groups. Tool inspectors show full results, retain unrecognized fields, stream updates, support independent scrolling/copying and become full-screen on narrow terminals. Tool errors are not presented as successful edits.
- Worker inspectors are read-only. Closing them does not stop tasks. They display actual recorded task input, assistant/thinking/tool messages and results, not inherited fork history; unavailable or partial records are identified explicitly.
- User-message fork actions create an independent session through the selected completed turn. The original session, draft and tasks are retained. Unsafe/incomplete history is rejected rather than guessed. Origin navigation only selects and scrolls the source; it does not change its tree position.
- Recent sessions preserve parent/child relationships. Rename/delete is identity-checked, deletion requires confirmation, and open writers are closed before deletion. General-purpose session browsing remains a frontend facility, independent of the optional Worker extension.

## Commands and preferences

| Command | Purpose |
| --- | --- |
| `/new`, `/resume`, `/sessions` | Start or browse sessions |
| `/model`, `/thinking` | Choose the current model and reasoning level |
| `/fork`, `/origin` | Create or inspect a session branch |
| `/tree` | Read-only session tree browsing |
| `/attach` | Attach an image |
| `/theme` | Preview/select a frontend theme |
| `/reload` | Reload frontend preferences, not backend extensions |
| `/codex-fast`, `/worker_settings` | Available only when their optional extension adapters are installed |

Keybindings use native Pi action names and the user's `keybindings.json`, including disabled/remapped actions. For example, session-picker deletion uses the configured `app.session.delete` action and requires confirmation. An explicit user override takes priority over defaults. See the [Chinese detailed reference](README.md#快捷键与配置) for the complete UI behavior catalogue.

Themes include `pi`, `catppuccin`, `tokyo-night`, `nord` and `rose-pine`. Theme selection previews immediately; Enter saves to frontend `goui.json`, Esc restores the previous theme. It does not rewrite native Pi theme settings. Transparent backgrounds remain supported.

Use native Pi for `/login`, `/logout`, full native settings and scoped-model management. After changing extension code, restart the affected Go backend; Go `/reload` does not reload Pi resources.

## Updates and compatibility

Startup may asynchronously report updates for the selected Pi installation and eligible global packages. This is notification-only: no installation or automatic upgrade. Current Pi release notes come from that installation's local changelog; offline update failures remain quiet.

Standard RPC select/confirm/input/editor, notifications, text statuses and widgets are supported. Arbitrary TypeScript TUI components, custom headers/footers/editors and complex widget layouts are not portable over RPC. This frontend does not claim complete native UI parity. Images in tool output remain attachments rather than inline pixels. Generic frontend facilities such as clipboard/editor integration, image previews, Git/quota display and update notices may use local I/O; only third-party extension business operations are restricted to their backend interfaces.

## Code layout

```text
internal/
├── rpc/                  # Pi transport and optional extension RPC envelope
├── extensions/           # Third-party protocols/clients/read-only projections
│   ├── askquestion/
│   ├── codexfast/
│   ├── worker/
│   ├── solpi/
│   ├── hostui/            # Third-party forms, controls and inspectors
│   └── toolrender/        # Third-party tool cards and full details
├── extensionapi/         # Neutral render types and host callbacks
├── session/              # Generic session/history projection
├── render/               # Layout and component rendering
│   ├── frame/
│   └── tooldetail/
└── ui/                   # Host state, input and event routing
    └── imagepreview/
```

Third-party protocol, UI and rendering implementations live under `internal/extensions/`: `hostui` owns forms/controls/inspectors, `toolrender` owns cards/full detail views, and the other subpackages own wire clients and read-only projections. Core UI/rendering packages retain only generic components and thin delegates. Small neutral callbacks bridge host operations without exposing the whole application Model. Authoritative execution and persistence remain in backend capabilities. See the architecture section below; AI modification guidance is in [AGENTS_EN.md](AGENTS_EN.md).

## Architecture and implementation boundaries

### Processes, transport and ownership

Each writable session owns a separate `pi --mode rpc` subprocess. Selecting an already-open session focuses its existing tab rather than creating another writer. Switching focus preserves every backend's draft, messages, queue, pending dialogs and stream. Startup neither injects extensions nor copies credentials, rewrites original settings or invokes a model. This frontend is not a sandbox: submitted prompts retain Pi's normal permissions and costs.

Requests use unique IDs; command responses and unsolicited events are handled separately. Writes are serialized and stdout/stderr are monitored. Cancellation, close and child exit clean up waiters and process groups. An explicit `PI_BIN` never silently falls back. Optional calls first discover the extension command `pi-ui-rpc` through `get_commands`; a missing bridge returns unavailable instead of sending an unknown slash prompt. Generic transport contains no Worker/Codex method names. Stopping a client wait does not roll back a committed backend operation.

### Core package contracts

These are the principal interfaces and invariants; source types remain authoritative.

| Package | Contract |
| --- | --- |
| `internal/rpc` | `Start(Config)` accepts the binary, cwd, agent/session paths, no-session flag and extra arguments. `Client.Call(ctx,request)` returns **response.data**; `Send` sends extension UI responses. `Events()` contains unsolicited events/errors only; `PID()` exposes the process ID and `Close()` is idempotent and bounded. `CallExtension` owns optional-bridge discovery, correlation and transport only. |
| `internal/session` | `State.Apply` projects streaming, lifecycle and queue events and indicates when an authoritative refresh is advisable. Busy clears on `agent_settled`, not prompt acknowledgement. `LoadState/LoadStats/LoadEntries/LoadMessages` load snapshots; `get_entries` returns `{entries,leafId}`, with messages used only as a fallback. Preserve original active-branch records across compaction rather than replacing them with summaries. MessageCount/MessageIndex count native records, not rendered rows. |
| `internal/config` | `Load(cwd)` and `AgentDir()` read configuration. `Bindings.Match/Keys/Actions` and `CanonicalKey/KeyChord` expose native action names and chords. Pi 1.5 defaults support keybindings.json string/array overrides; [] disables an action. Themes use agent presets or embedded fallbacks; Theme supplies text, role-background, border and status colors. Global/project settings follow loader merge rules. Loading is read-only; explicit preference saves are separate operations. |
| `internal/input` | `Editor` wraps textarea through `New/Update/SetValue/Value/Jumping`, implementing native named editing actions, newline, typing/paste and kill/yank/undo. Bubbles defaults must not bypass disabled/remapped actions. The host owns history, submit, completion, modal and global actions. Disable the virtual cursor; translate Area.Cursor through actual layout geometry into `tea.View.Cursor` for terminal IME anchoring. |
| `internal/render` | Pure views such as `Transcript(items,theme,opts)` return Document/Text/Blocks/MessageCount. Blocks retain ID, role, start/end, MessageIndex and effective fold state. Total width including padding stays within the requested width (minimum 1). Hidden thinking remains in the model; history is not hard-truncated. Markdown renderers may be cached per width, never sharing mutable state unsafely. |

`session.ReadFile` returns a get_entries-compatible read-only snapshot. `List(root,cwd)` lists native JSONL under root/sessions; empty cwd means all. Explicit rename/delete are the only history-write exceptions: validate closed v3 files and file identity, append native session_info for rename, and unlink only the selected file without recursive deletion. Rename open sessions through RPC; deletion waits for the writer to close.

`render.Sidebar` consumes read-only state and SessionRow values. SESSION holds name/cwd/optional Git/context, STATS uses two columns, and eligible Codex quota is optional. RECENTS, New and open-session cards follow the UI layout above. Model/thinking controls remain in the input footer, with no files/themes/plugins tabs. The host may hide the sidebar on narrow terminals.

### Cohesion and dependency direction

Directories follow behavioral ownership rather than file count. Extract meaningful behavior behind small interfaces; do not expose Model internals or add empty forwarding layers just to move files.

- Backend capabilities own execution, validation and persistence. Under `internal/extensions`, `worker` obtains read-only transcripts through RPC, `codexfast` does not access configuration files, `askquestion` does not authoritatively validate answers, and `solpi` does not perform compaction. Worker replay may depend on generic session projection, never the reverse. `extension_plan.go` only connects optional plan presentation.
- `hostui` owns third-party forms, controls and inspectors without importing `internal/ui` or receiving the whole Model. `toolrender/cards` and `toolrender/detail` use instance-local `extensionapi` callbacks, never importing parent render, tooldetail or ui, or introducing a mutable global rendering registry. Core packages retain generic components and thin bindings.
- `render/frame` owns Border, Box, Gradient and Palette: frame geometry, role palettes, theme presets and ANSI-safe gradients. Both render and UI consume it directly; it does not depend on its parent. Higher-level transcript/sidebar geometry stays in render.
- `render/tooldetail` keeps generic/native full detail documents independent of UI state; third-party details live in `extensions/toolrender/detail`.
- `ui/imagepreview` has a zero-value Store.Open/Close for image validation, private caches and viewer launch/cleanup. MaxBytes is shared with image input. It depends on session image data, not Bubble Tea or parent UI; the host schedules commands and routes results to the originating tab.

Clipboard, editor, images, Git, quota, updates, generic session browsing and frontend preferences may still use local I/O. Separation applies to third-party business rules and private storage, not every frontend auxiliary feature. Missing capabilities preserve ordinary RPC: hide unavailable Codex controls, fall back to saved Worker results, and use generic views for unknown tools/dialogs. No checkout layout or author configuration is required.

### Host UI lifecycle

`cmd/pi-goui` and `internal/ui` own layout, input, event routing, focus, scroll/fold hit testing, completion, native actions and full-history refresh on startup/resume/settled. Popups sit above the input while preserving the backdrop. Slash matching is fuzzy (Enter executes, Tab completes), and file indexing is asynchronous and bounded. Abort via Esc takes priority over completion while a prompt is active/pending.

Preserve user overrides such as Ctrl+Y for resume; `/reload` reloads frontend settings. Extension select/confirm/input/editor requests, notifications and asynchronous results remain scoped to their originating session. Resume activates an existing target or reuses an empty slot with a fresh internal ID; retain nonempty sessions as described above, requiring confirmation for explicit loss of drafts/tasks. Rename/delete remain selection-bound and reserve their target against concurrent in-app mutation/resume. Bubble Tea handles exit/suspend; final close cleans up every child.

Algorithm/lifecycle tests live beside their owning subpackage; component rendering, click geometry and event-routing integration tests remain with the host. Prioritize missing capabilities, unknown protocols, RPC failure/cancellation, late cross-tab events, read-only history and empty user profiles. Real-host checks forbid model/tool calls. Advanced native TUI is not an RPC protocol; passing tests does not establish every platform or complete native-UI parity. AI modification guidance is in [AGENTS_EN.md](AGENTS_EN.md).

## Verification

```bash
cd /path/to/pi-goui
go test -race ./...
go vet ./...
python3 script/test-ui.py
# Optional read-only real-Pi checks; isolated HOME and extensions disabled:
python3 script/test-ui.py --real-pi "$(command -v pi)"
python3 script/test-ui.py --real-only --real-pi "$(command -v pi)"
```

The PTY harness uses a temporary build, isolated HOME and fake RPC. Its real-Pi guard only permits read-only queries and rejects prompt/tool operations. Automated success is not a claim that every terminal, platform or manual interaction was tested.

Current verification: Go unit/race tests and vet, plus a detached binary's read-only `--check` in an empty user profile, pass. The PTY acceptance script still fails at `Expected visible: Choose model`; the same failure was reproduced from the unmodified HEAD baseline. Do not treat the documented PTY command as a passing check until its fixture issue is resolved. For suites that spawn many instrumented helper processes, `GORACE=atexit_sleep_ms=0 go test -race ./...` avoids the race runtime's per-process exit delay without disabling race detection.

## Credits

The initial component split and RPC lifecycle reference [cavaldos/pitago](https://github.com/cavaldos/pitago); its MIT notice is preserved in [LICENSE](LICENSE). Other repository UI experiments were design references, not runtime dependencies. This frontend does not replace or install Pi itself.
