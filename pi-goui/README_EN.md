# pi-goui

[简体中文](README.md)

An independent Go / Bubble Tea frontend using standard Pi RPC for models, tools, authentication and extensions. Copy the entire `pi-goui/` directory to build, or distribute the binary alone; no other checkout directories or author-specific settings are required at runtime.

## Build and run

Building requires Go 1.26+; running requires an installed, authenticated Pi / Node.js environment. From this directory:

```sh
go build -trimpath -o bin/pi-goui ./cmd/pi-goui
./bin/pi-goui --cwd /path/to/project
./bin/pi-goui --check
```

Alternatively, `./run.sh` builds and starts the frontend. Common options:

| Option | Purpose |
| --- | --- |
| `--pi /path/to/pi` / `PI_BIN` | Select the Pi executable; otherwise use PATH |
| `PI_CODING_AGENT_DIR` | Select the agent directory; defaults to `~/.pi/agent` |
| `--continue` / `-c`, `--session file.jsonl` | Resume the latest session in this cwd, or open a specific session |
| `--no-session` | Disable persistence; cannot be combined with resume options |
| `--check` | Check RPC startup without sending a prompt or saving a session |
| `-- --model provider/model --no-extensions` | Forward Pi options; cannot override RPC mode/session ownership or pass positional prompts |

Authenticate in native `pi`; do not copy someone else's credentials or settings. Starting the frontend does not automatically install extensions or invoke a model. **Submitted prompts retain Pi's normal permissions and may incur costs; this is not a sandbox.**

## Usage and configuration

Each open session owns an RPC process. Switching tabs preserves drafts, queues, dialogs and running tasks; reopening a session focuses its existing tab rather than creating a second writer. Do not modify the same session file simultaneously from another Pi process.

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

- [cmd/pi-goui/](cmd/pi-goui/): CLI and startup.
- [internal/core/](internal/core/): generic configuration, RPC, sessions, input, rendering and UI; `extension/` owns integration contracts and `extensionapi/` low-level projection/drawing interfaces.
- [internal/extensions/](internal/extensions/): third-party protocols, state, UI and rendering; exports an interface slice for core to compose.

Only `core/` and `extensions/` belong directly under `internal/`. See the [extension guide](internal/extensions/README_EN.md) for integration design and [AGENTS_EN.md](AGENTS_EN.md) for development constraints.

## Verification

Commands and isolation requirements are in [AGENTS_EN.md](AGENTS_EN.md#verification). Tests default to fake RPC; optional real-RPC checks must use the read-only allowlist proxy, never model/tool calls. Passing tests does not establish real-terminal or cross-platform acceptance.

## Credits

Structure and RPC lifecycle draw on [cavaldos/pitago](https://github.com/cavaldos/pitago). Its MIT notice is retained in [LICENSE](LICENSE).
