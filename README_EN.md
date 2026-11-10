# Pi Coding Agent Configuration

[中文](README.md)

This is my personal [Pi Coding Agent](https://pi.dev) configuration and standalone terminal frontend: `pi-goui`, custom extensions, a theme, keybindings, and workflows, not a general-purpose distribution. This repository is for configuration source code safe to share. Extensions have local system access; review their code before running them, especially third-party extensions.

## Features

- **[pi-goui](pi-goui/README.md)**: a standalone Go / Bubble Tea v2 frontend, with a separate `pi --mode rpc` subprocess per session. It reuses Pi's agent, tools, authentication, configuration, and extensions. The sidebar shows sessions, directories, CTX, usage, and eligible Codex quota; the background is transparent by default.
- Click the sidebar directory in a new, empty session to set its cwd, with directory suggestions, Tab completion, and navigation. Drafts, images, queues, scrolling, and fold state are independent per session; switching tabs does not interrupt background work.
- `ask_question` supports single-choice, multiple-choice, and multi-question confirmations; concurrent questionnaires are queued to avoid replacing each other.
- `/codex-fast` separately configures Fast / Ultrafast request tiers; Automatic Chinese session naming is enabled by default and can be disabled in its configuration.
- [Worker](#worker) assists with scoped tasks in separate Pi subprocesses. The main model explicitly selects one of three tiers: Fast, Normal, and Deep; Thinking strength is configured independently. Path checks are not a security sandbox; inspect the resulting changes.
- [SoL-Pi](#context-management-sol-pi) archives large tool results and provides online context compaction. Tool-result filtering reduces exposure to common secrets but does not guarantee redaction.

## Local extension inventory

Each row corresponds to current source under `extensions/`, excluding `node_modules`, tests, and helper modules. Pi discovers top-level `.ts` files and directories containing `index.ts`. Do not load internal entries a second time.

| Extension / source | Purpose | Entry point | Configuration / activation |
| --- | --- | --- | --- |
| [ask-question](extensions/ask-question/index.ts) | Queued single-choice, multiple-choice, custom-input, or multi-question forms | Agent tool `ask_question`, not a slash command | No separate configuration; TUI uses the full custom form, while RPC/Pi Web uses standard dialogs for single choice, custom input, sequential questionnaires, and emulated multiple choice; modes without UI return unanswered questions only |
| [autoname](extensions/autoname/index.ts) | Generate/update Chinese session names while preserving manual names | Automatic after the run fully settles | `autoname.json`: `enabled`, `notify`, `cooldownSeconds`, `model`, `reasoning` |
| [codex-fast](extensions/codex-fast/index.ts) | Set Fast / Ultrafast for eligible Codex requests | Only the `/codex-fast` standard selection menu | `codex-fast.json`: independent boolean switches `fast`, `ultrafast`, both off by default |
| [filter-output](extensions/filter-output/index.ts) | Filter common sensitive text and certain sensitive-file reads before successful tool results reach the model | Automatic `tool_result` hook, not a user command | No separate configuration; redaction is not guaranteed; errors are not filtered and `.env.example` reads bypass filtering |
| [herdr-agent-state](extensions/herdr-agent-state.ts) | Report working/blocked/idle state and session references to Herdr's local socket | **Internal bridge, not a user command** | `HERDR_ENV=1` with `HERDR_SOCKET_PATH` and `HERDR_PANE_ID`; binds only to the main session with UI; managed/overwritten by Herdr |
| [sol-pi](extensions/sol-pi/index.ts) | ObservationPack archives large tool results; Online Context Compact compacts context online | Automatic context hooks; agent tools `obs_recall`, `update_plan` | Auto-discovered entry; do not load twice. See [SoL-Pi docs](extensions/sol-pi/README.md) |
| [telegram](extensions/telegram/index.ts) | Send run replies and some question notifications to a target chat | Automatic event notifications, not a user command; no remote control | Environment variables `PI_TG_TOKEN`, `PI_TG_CHAT`; sends nothing if either is missing; no polling; depends on `node-telegram-bot-api` |
| [worker](extensions/worker/index.ts) | Bounded subprocess tasks, parallel scheduling, and progress cards | Agent tool `worker`; `/worker_settings` | `worker-settings.json`: required three-tier models/Thinking, concurrency, timeout, and output limit; in subprocesses, this entry installs only the write guard, not nested delegation |

The former native UI has been archived unchanged from `extensions/ui/` to [`ui/`](ui/README.md) at the repository root and **is no longer automatically loaded**. Active extensions use shared frame and token helpers in [`extensions/shared/`](extensions/shared/README.md), which has no extension entry point and is not loaded independently. The archive retains its original relative imports: move it back to `extensions/ui/` to restore the native UI; do not load the root archive directly. Moving files does not unload patches in running processes; restart Pi / `pi-goui`.

The `btw` extension has been removed; temporary Q&A via `/btw` or `Ctrl+B` is no longer provided. The old local `context`, `usage`, and `fast` extensions have been removed; their `/context`, `/usage`, and `/fast` commands are no longer provided by these extensions. `pi-goui` displays context usage in the sidebar; Fast settings use `/codex-fast`. `rolling-compact` and its configuration have been removed, and `/roll-compact` is no longer provided; context management uses SoL-Pi again. External packages are not part of this local inventory.

## Installation

Requires Node.js **22.19+** and [Pi Coding Agent](https://github.com/earendil-works/pi); `pi-goui` additionally requires **Go 1.26+**. Git is used for Worker change checks and read-only repository status in the sidebar.

```bash
npm install -g --ignore-scripts @earendil-works/pi-coding-agent
```

Back up your existing personal agent directory first (if present). Ensure the backup destination does not exist to avoid overwriting it. Replace `<repository-url>` with this repository's URL:

```bash
mv ~/.pi/agent ~/.pi/agent.backup
git clone <repository-url> ~/.pi/agent
npm ci --prefix ~/.pi/agent/extensions
```

On first use, authenticate with `/login` in native Pi:

```bash
pi
```

Then launch the Go frontend from your project directory:

```bash
~/.pi/agent/pi-goui/run.sh                         # Use the caller's current directory
~/.pi/agent/pi-goui/run.sh --cwd /path/to/project
~/.pi/agent/pi-goui/run.sh -c                      # Resume this directory's latest session
~/.pi/agent/pi-goui/run.sh --session /path/to/session.jsonl
~/.pi/agent/pi-goui/run.sh --check                 # Read-only RPC check; no prompts
```

`run.sh` builds `pi-goui/bin/pi-goui` before each launch. Set `PI_BIN` or `--pi` to choose the Pi executable; `--no-session` disables session persistence. Native `pi` remains available independently, without automatically loading the old UI.

Skip `mv` if `~/.pi/agent` does not exist. `npm ci` installs local dependencies from the lockfile in `extensions/` (runtime dependencies `typebox` and `node-telegram-bot-api`, plus Pi SDK / TypeScript development dependencies); it does not upgrade global Pi. Once Pi starts, it separately manages the `pi-web-access` extension package declared in `settings.json`. Native Pi can reload extensions, Skills, themes, and keybindings with `/reload`. In `pi-goui`, `/reload` only rereads frontend configuration; restart the frontend after changing extensions or frontend code to start fresh RPC backends. Local SDK development dependencies in the lockfile do not replace the validated global host version. SoL-Pi has been validated on global Pi **1.0.4**, and its isolated test runner requires that version. See [SoL-Pi validation instructions](extensions/sol-pi/README.md#offline-validation).

### Update notices and release notes

Startup checks Pi and user-level external packages in the background. Available updates appear below the default welcome content in the chat area: **notification only, with no popup or installation**. On the first launch after a Pi update, the same area shows the installed version's bundled release notes, with scrolling for longer entries. First use also shows them once; later launches do not repeat them. Extension release notes are not shown.

Failed checks and unchanged versions stay quiet. Offline mode skips network checks but still allows local Pi release notes. Only the frontend's separate cache records the displayed version; Pi settings and conversation history are unchanged. See [startup update notices](pi-goui/README.md#启动更新通知与-pi-变更内容).

## Common commands

| Command | Purpose |
| --- | --- |
| `/login` | Authenticate in native Pi only; the frontend reuses existing login |
| `/model`, `/thinking` | Choose a model or reasoning strength in `pi-goui` |
| `/new`, `/sessions`, `/resume` | Create an independent session, select or resume history |
| `/name` | Name the current session |
| `/attach path`, `/detach` | Insert image references, clear pending images |
| `/hotkeys` | Show effective keybindings |
| `/worker_settings` | Adjust Worker models, concurrency, and other settings |
| `/codex-fast` | Open the Fast / Ultrafast settings menu |
| `/reload` | Reread frontend configuration in `pi-goui`; native Pi has a different reload scope |
| `/close`, `/quit` | Close the current session, exit the frontend and clean up subprocesses |

Session, input, and display commands above are handled by `pi-goui`; extension settings commands go to the Pi RPC backend. Use native Pi for `/login`, `/logout`, the full native settings UI, and complex TypeScript `custom()` interfaces that are not guaranteed to work over RPC. Standard select, confirm, input, and editor dialogs are supported. `ask_question`, `worker`, `obs_recall`, and `update_plan` are agent tools, not slash commands.

### Interface and questionnaire interaction

The following describes current `pi-goui`, not the archived native UI:

- **Sidebar and input**: SESSION shows the name, cwd, and CTX; STATS shows `in/out/cache/cost`. Use `+ New session` to create a session. The purple input frame contains shortcuts and clickable model/reasoning controls. The working indicator appears only during activity; completion metrics remain until new input.
- **Setting cwd**: click the SESSION directory only in a new session with no messages, queued work, or running tasks. The dialog accepts absolute paths, paths relative to the current cwd, and `~/`. It lists directories automatically: ↑↓ selects, Tab completes, clicking a directory enters it, and `../` goes up. Enter applies **the directory in the input field**; Esc cancels. Invalid paths keep the dialog open with an error. A valid change replaces only that session's backend, preserving unsent drafts and images without changing the global cwd or other sessions.
- **Messages and tools**: user, assistant, and system frames use different gradient borders, titled “你”, the current model name/ID, and “系统”. All tool calls belong to groups. Without following commentary, a group automatically previews its last three calls and counts as expanded: click the group to collapse everything, then click again to expand it fully. Manual folds take priority. Live Workers appear independently and rejoin adjacent groups on completion. `Ctrl+O` toggles tool expansion; `Ctrl+T` toggles thinking visibility.
- **Session isolation**: each tab has its own backend and UI state. Resuming history replaces the tab in place, with confirmation for drafts or active work; an already-open target is simply activated. In the session picker, `Ctrl+R` renames and `Ctrl+D` confirms deletion, stopping the corresponding writer first. Process isolation is not a filesystem sandbox.
- **Input and images**: `/` provides fuzzy command completion; `@file` searches and inserts paths. Clipboard images or `/attach` insert blue markers such as `[image1]` at the cursor and send images with the same text message. Removing a marker excludes that image. After sending, click a marker to open a validated private temporary copy in the system viewer, without rereading the source file or uploading it. Viewing is supported on Linux/macOS; frontend exit removes temporary files.
- **Dialogs and questionnaires**: dialogs are horizontally centered with their bottom edge against the input frame, leaving chat visible behind them. Questionnaires are queued; RPC standard dialogs differ from the native custom UI. Native UI components cannot be reused directly in Go.
- **Keybindings**: native action names and `keybindings.json` overrides are respected; `[]` disables an action. This repository binds `Ctrl+Y` to session resume. Esc aborts the current task; Shift+Tab cycles reasoning strength. Only explicitly pressing `Ctrl+S` saves the default reasoning strength.

See the [pi-goui documentation](pi-goui/README.md) for full usage and RPC boundaries.

### Codex Fast

`/codex-fast` only opens a standard selection menu, available in native Pi and through RPC; it does not accept subcommands such as `on` / `off`. Select Fast or Ultrafast with the arrow keys. **Enter toggles that switch and saves immediately** to `codex-fast.json`, keeping the menu open. **Esc exits** without undoing saved toggles. Menu saves take effect immediately. After editing the configuration file manually, use `/reload` in native Pi or restart the `pi-goui` backend.

Requests are modified only when the provider is `openai-codex` and the API is `openai-codex-responses`. If Ultrafast is enabled and the ID **exactly equals `gpt-6-astra`**, the payload uses `service_tier: "ultrafast"`; otherwise it falls back to `"priority"` only if Fast is enabled. Ultrafast alone leaves requests unchanged for nonmatching models. This is not a retry/downgrade after server rejection and guarantees neither server support nor speed.

The old native footer's ✨ / 🌟 switch markers belong to the archived implementation; `pi-goui` does not display them. Check the `/codex-fast` menu or saved configuration instead of inferring a request tier from the model name or frontend state.

### Remaining Codex subscription quota

`pi-goui` shows remaining quota percentages, reset countdowns, and integer Credits in the sidebar instead of the old footer badge. Queries require the current model to use the official `openai-codex` origin and an existing usable OAuth login; custom origins and API keys do not trigger quota requests.

The frontend reads existing authentication and asynchronously queries the fixed `https://chatgpt.com/backend-api/wham/usage` endpoint roughly once a minute. It neither copies nor refreshes credentials, uses no proxy, and follows no redirects. Pi must handle expired login; unavailable fields use placeholders rather than fabricated quota.

### Other default behavior

These are source-code defaults, not copied local model or user settings; local configuration can override them.

- **Autoname** is enabled with notifications by default, using the current model, minimal reasoning, and a 600-second cooldown. Automatic naming runs only after a session with UI fully settles and preserves manual names.
- **Telegram** sends nothing without both environment variables; it only notifies and does not receive chat commands. The current question watcher recognizes only the top-level `question` in `ask_question`; multi-question `questions` forms do not produce matching question notifications. Failures are silently ignored; delivery is not guaranteed.

## Worker

`worker` delegates bounded, independently verifiable subtasks to separate Pi subprocesses. Every task requires explicit `sessionMode` and `preset`. The main model prefers `fork` (an isolated snapshot of the current active branch) and uses `new` (fresh history) only when little context handoff is needed. Child sessions are temporary and do not automatically load Skills or Context Files, and may not create or call other Workers. The main agent decomposes the work, handles blockers, inspects the actual diff, verifies it, and makes the final acceptance decision.

Starting, continuing to wait, and cancelling all use the single `worker` tool. Each main session allows only one unfinished batch; always pass a `tasks` array, even for one task; put parallel work in a single call. Batch results retain input order and each task may complete, be blocked, or fail independently.

The summary and `changed_files` report Worker claims and workspace snapshot information; they do not replace the main agent's inspection of the actual diff.

Five modes are supported: `scout` for read-only investigation, `implement` for implementation, `test` for testing, `review` for read-only review, and `fix` for confirmed issues. Write modes must declare non-empty `allowedPaths`; `relevantFiles` are read hints and grant no write access. `allowedPaths` and `forbiddenPaths` are relative to `task.cwd`: `backend/file.ts` matches exactly one file (including a new file), while directory descendants require `backend/**`. Existing bare directories such as `backend`, `./backend`, or `backend/` fail validation before any batch task starts, with a `backend/**` suggestion. Use `backend/**` even for directories not yet created; a nonexistent bare path is not assumed to be a directory. Use `/**` to exclude directory descendants in `forbiddenPaths` too.

### Tiers and settings

- **Fast**: clear, local work that is easy to verify.
- **Normal**: routine work and the default when unsure between Fast and Deep.
- **Deep**: difficult work such as cross-module changes, unknown root causes, or concurrent state; it is the highest task-complexity tier.

`preset` is required and accepts only `fast`, `normal`, and `deep`; **there is no auto or Max execution tier**, and no keyword or file-count inference. Thinking levels `off/minimal/low/medium/high/xhigh/max` are independent per-tier reasoning strengths, not extra routing tiers, and must be supported by the model.

Models and Thinking for every tier are read exactly from local `worker-settings.json`, without automatic conversion, upgrades or downgrades; `fast`, `normal`, and `deep` must each specify a valid model. Missing/unavailable models produce errors, never guessed routes or silent downgrades. Loading legacy configuration removes and persists the obsolete top-level `max` and `automaticDelegationEnabled` entries. Delegation is always supported, with no enable switch or `manual` parameter. General source defaults are concurrency 3, 15-minute task timeout, and 64 KiB output limit. `/worker_settings` interactively edits these settings and per-tier models/Thinking; choosing “Save and exit” applies them immediately to subsequent tasks.

### Blockers and user decisions

`worker` supports only start, wait for completion, and cancel; there is no interactive child-to-parent Q&A. A task missing information, decisions, or permission returns `blocked` and releases its resources. The main agent handles blockers, uses `ask_question` for user decisions if needed, and starts a new bounded task. Cancellation is not consent; scope expansion requires cancelling affected batches and awaiting cleanup. User interaction does not extend task deadlines.

### Parallelism, progress, and acceptance

Batch tasks are scheduled up to the concurrency limit: read-only tasks may run in parallel; read/write tasks conflict and must run serially; writers run in parallel only when `cwd` and all declared write paths can be resolved and proven disjoint. Unprovable independence is treated as a conflict; scheduling does not promise independent Git worktrees. Sessions in the same process own separate children, slots, and cleanup. Separate Pi windows are independent but may still share a worktree; path locks do not span windows.

`pi-goui` renders real Worker details: objectives, status, model, session mode, usage, elapsed time, completion summaries, and recent activity, with task-level folding. Live tasks expand by default and rejoin tool groups on completion; manual folds take priority. It renders RPC data itself without loading the archived UI. Active TypeScript extensions use shared rendering helpers in `extensions/shared/`.

Progress is truncated and filtered for common sensitive fields; this does not guarantee complete redaction. Cancellation, timeout, or output overflow triggers an attempt to terminate the subprocess; cancel calls wait for cleanup. Failures go to the main agent for handling and are not automatically retried. Reloading, switching sessions, or navigating the session tree within a Pi backend cleans up background tasks. Merely switching frontend tabs in `pi-goui` does not trigger that backend lifecycle; background sessions keep running. Historical cards use persisted snapshots on the current branch and never resume subprocesses.

`cwd` must remain inside the main working directory. `allowedPaths`/`forbiddenPaths` are checked only before `edit` and `write` tool calls. They are not a shell or filesystem sandbox and do not constrain `bash` or other extension tools; do not treat them as an isolation boundary. The main agent must inspect actual changes, pre-existing worktree modifications, verification evidence, and task scope before accepting the result.

Failure diagnostics retain termination sources and necessary process identifiers; they cannot establish why historical runs were cancelled. Custom SDK hosts must emit and await `session_shutdown`: bare `AgentSession.dispose()` in Pi 0.87.1 does not emit it. See the [Worker extension documentation](extensions/worker/README.md) for lifecycle details, host limitations, and test commands.

## Configuration and security

- `settings.json` declares Pi extension packages and theme/display preferences. `worker-settings.json`, `codex-fast.json`, and `autoname.json` control their respective features. `keybindings.json` configures `Ctrl+Y` session resume; theme source is [themes/pi.json](themes/pi.json).
- Keep login credentials and model secrets (such as `auth.json`, `models.json`, and `models-store.json`), sessions (such as `sessions/`) in ignored local files; do not commit or copy them into a public repository.
- Create custom model configuration locally and prefer environment-variable references for secrets; see [model configuration](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/models.md).
- `.gitignore` does not protect tracked files; check `git status --short --ignored` and `git diff --cached` before publishing. If credentials were exposed, remove the public content and rotate them.
- Telegram notifications are not used by default. To enable them, first review the extension source and securely configure the chat target and Bot Token; **never put credentials in the repository**. Notifications may send task input, replies, and questions to the target chat, and delivery is not guaranteed.
- Automatic session naming may send conversation content to its model, with extra requests and costs; check recipients before use. Treat Herdr session references as sensitive data too.
- Tool-result filtering is heuristic and may miss secrets or mask ordinary content. It cannot replace credential management, pre-commit review, or restrictions on extensions' local permissions.

## Context management: SoL-Pi

[SoL-Pi](extensions/sol-pi/README.md) loads automatically through `extensions/sol-pi/index.ts`; do not explicitly load its internal entries again. This local version includes only ObservationPack and Online Context Compact, not Action Fusion, Reducer, or the upstream global configuration loader.

- **ObservationPack**: plain-text, non-error tool results larger than 10 KiB remain full text for two provider requests, then become stable ID and head/tail references. The model can retrieve the originals in pages using `obs_recall`. Short text, errors, mixed media, and recall results are not packed; raw session history is not rewritten.
- **Online compaction**: runs only in persistent main sessions, not Workers. `update_plan` must submit the complete plan; completing a previously registered unfinished step while work remains can trigger compaction and continuation only after economic checks pass. Completing the whole plan does not trigger it. Summaries may lose detail and incur extra model costs; savings are not guaranteed. This is not the former `/roll-compact` manual-summary workflow. Tool calls are rendered by the active frontend, without relying on the old UI's hiding patches; there are no promotional banners or savings notices.
- **Local archives**: large results are stored under `sol-pi/<session-id>/observation-pack/` in the session directory. Non-persistent sessions use a temporary directory that survives exit, but mappings are not restored after a process restart. Treat these originals as sensitive data; they are not automatically deleted.

See [SoL-Pi validation](extensions/sol-pi/README.md#offline-validation) for commands and version restrictions.

## Frontend validation

Run from `pi-goui/`:

```bash
go test -race ./...
go vet ./...
go build -trimpath -o bin/pi-goui ./cmd/pi-goui
python3 script/test-ui.py --binary bin/pi-goui
```

Default validation uses fake RPC and isolated PTYs, without paid model calls. Optional real-RPC checks run through a read-only allowlist proxy; see [pi-goui validation](pi-goui/README.md#验证). See each extension's documentation for its validation commands and host-version constraints.
