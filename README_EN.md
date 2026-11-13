# Pi Coding Agent Configuration

[中文](README.md)

This is my personal [Pi Coding Agent](https://pi.dev) configuration and standalone terminal frontend: `pi-goui`, custom extensions, a theme, keybindings, and workflows, not a general-purpose distribution. This repository is for configuration source code safe to share. Extensions have local system access; review their code before running them, especially third-party extensions.

The reusable components are independent: [pi-goui](pi-goui/README_EN.md) works outside this checkout, and the [optional extension package](extensions/README_EN.md) can be installed into any user's Pi environment. Root configuration files remain personal examples, not installation prerequisites. Repository-specific Go integrations live in [third-party adapters](pi-goui/internal/extensions/README_EN.md).


## Features

- **[pi-goui](pi-goui/README_EN.md)**: a standalone Go / Bubble Tea v2 frontend, with a separate `pi --mode rpc` subprocess per session. It reuses Pi's agent, tools, authentication, configuration, and extensions. The sidebar shows sessions, directories, CTX, usage, and eligible Codex quota; the background is transparent by default.
- Click the sidebar directory in a new, empty session to set its cwd, with directory suggestions, Tab completion, and navigation. Drafts, images, queues, scrolling, and fold state are independent per session; switching tabs does not interrupt background work.
- `ask_question` supports single-choice, multiple-choice, and multi-question confirmations; concurrent questionnaires are queued to avoid replacing each other.
- `/codex-fast` separately configures Fast / Ultrafast request tiers; Automatic Chinese session naming is enabled by default and can be disabled in its configuration.
- [Worker](#worker) assists with scoped tasks in separate Pi subprocesses. The main model explicitly selects one of three tiers: Fast, Normal, and Deep; Thinking strength is configured independently. Path checks are not a security sandbox; inspect the resulting changes.
- [SoL-Pi](#context-management-sol-pi) archives large tool results and provides online context compaction. Tool-result filtering reduces exposure to common secrets but does not guarantee redaction.

## Local extension inventory

Each row corresponds to current source under `extensions/`, excluding `node_modules`, tests, and helper modules. Pi discovers top-level `.ts` files and directories containing `index.ts`. Do not load internal entries a second time.

| Extension / source | Purpose | Entry point | Configuration / activation |
| --- | --- | --- | --- |
| [ask-question](extensions/ask-question/index.ts) | Questionnaire data, queuing, cancellation, and answer validation | Agent tool `ask_question`; `ask-question:v1` service | Interaction comes from `pi-ui` or another adapter; without one, returns unanswered questions and `unavailable` |
| [autoname](extensions/autoname/index.ts) | Generate/update Chinese session names while preserving manual names | Automatic after the run fully settles | `autoname.json`: `enabled`, `notify`, `cooldownSeconds`, `model`, `reasoning` |
| [codex-fast](extensions/codex-fast/index.ts) | Apply Codex request tiers; validate and persist configuration | `codex-fast` service; `/codex-fast` belongs to `pi-ui` | `codex-fast.json`: `level` is `normal`, `fast`, or `ultrafast`; legacy booleans remain readable |
| [filter-output](extensions/filter-output/index.ts) | Filter common sensitive text and certain sensitive-file reads before successful tool results reach the model | Automatic `tool_result` hook, not a user command | No separate configuration; redaction is not guaranteed; errors are not filtered and `.env.example` reads bypass filtering |
| [sol-pi](extensions/sol-pi/index.ts) | ObservationPack archives large tool results; Online Context Compact compacts context online | Automatic context hooks; agent tools `obs_recall`, `update_plan` | Auto-discovered entry; do not load twice. See [SoL-Pi docs](extensions/sol-pi/README_EN.md) |
| [telegram](extensions/telegram/index.ts) | Send run replies and some question notifications to a target chat | Automatic event notifications, not a user command; no remote control | Environment variables `PI_TG_TOKEN`, `PI_TG_CHAT`; sends nothing if either is missing; no polling; depends on `node-telegram-bot-api` |
| [worker](extensions/worker/index.ts) | Bounded subprocess tasks, scheduling, and structured state | Agent tool `worker`; `worker:v1` service; `/worker_settings` belongs to `pi-ui` | `worker-settings.json`: models/Thinking, concurrency, timeout, and output limit; subprocesses install only the write guard, not nested delegation |
| [pi-ui](extensions/pi-ui/index.ts) | Native rendering, forms, menus, notifications, and refresh; legacy Go/generic RPC bridges | Optional independent extension; never replaces capability tools | Auto-discovered; depends on capability interfaces, never the reverse |

Active native UI lives in [`extensions/pi-ui/`](extensions/pi-ui/README_EN.md); the old `extensions/ui/` implementation is not needed or shipped here. [`extensions/shared/`](extensions/shared/README_EN.md) contains only capability contracts, fact events, session policy and pure token estimation, with no extension entry point. Restart the Pi backend (or use native Pi `/reload`) after changing extension code.

The `btw` extension has been removed; temporary Q&A via `/btw` or `Ctrl+B` is no longer provided. The old local `context`, `usage`, and `fast` extensions have been removed; their `/context`, `/usage`, and `/fast` commands are no longer provided by these extensions. `pi-goui` displays context usage in the sidebar; Fast settings use `/codex-fast`. `rolling-compact` and its configuration have been removed, and `/roll-compact` is no longer provided; context management uses SoL-Pi again. External packages are not part of this local inventory.

Third-party UI authors can follow an extension's own bilingual README without reverse-engineering this frontend. See the [per-extension integration index](extensions/README_EN.md#third-party-ui-integration-one-readme-at-a-time). Structured RPC UIs use the neutral `PI_UI_PROTOCOL=1` opt-in; Go protocol names remain as compatibility wire identifiers. All Go-specific third-party protocol, interaction and rendering implementations now live under `pi-goui/internal/extensions/` (`hostui/` and `toolrender/`), not scattered across the core UI.

Copy-only use is supported: copy `extensions/<module>/` plus `extensions/pi-ui/<module>/` into `<target>/<module>/` and `<target>/ui/<module>/`; no `shared/` or whole-package UI is required. Each module README documents native registration and RPC. AI contributors adding Go integrations should read [`pi-goui/AGENTS_EN.md`](pi-goui/AGENTS_EN.md).

## Installation

Requires Node.js **22.19+** and [Pi Coding Agent](https://github.com/earendil-works/pi); `pi-goui` additionally requires **Go 1.26+**. Git is used for Worker change checks and read-only repository status in the sidebar.

```bash
npm install -g --ignore-scripts @earendil-works/pi-coding-agent
```

Clone to any source directory; **do not replace your existing `~/.pi/agent` or copy the author's settings, models, or credentials**:

```bash
git clone <repository-url> /path/to/pi-config
```

**Use only the standalone Go frontend** (the `pi-goui/` directory can be copied out independently):

```bash
/path/to/pi-config/pi-goui/run.sh --cwd /path/to/project
/path/to/pi-config/pi-goui/run.sh --check
# Or build a portable executable:
go -C /path/to/pi-config/pi-goui build -trimpath -o /path/to/pi-goui ./cmd/pi-goui
```

It uses the current user's Pi executable and agent directory. Set `PI_BIN` / `--pi` or `PI_CODING_AGENT_DIR` when needed. Authenticate with `/login` in native Pi. Missing repository themes use embedded defaults; these extensions are not required.

**Optionally install the extension package**:

```bash
npm ci --prefix /path/to/pi-config/extensions
pi install /path/to/pi-config/extensions
```

This registers the package in your Pi settings; it does not copy the author's configuration. Configure your own models and Worker tiers before using Worker. See the [bilingual extension guide](extensions/README_EN.md) for selective loading, prerequisites, update and uninstall instructions. Do not register the package twice if the same source is already auto-discovered in your agent directory.

The current type checks and isolated installation/RPC checks use Pi SDK **1.5.2** and Node **22.19**. The historical SoL-Pi suite targets Pi **1.0.4** and has reproduced baseline failures on the current host; this is not a claim that its full suite passes. See [validation limits](extensions/sol-pi/README_EN.md#offline-validation).

Native Pi `/reload` reloads extensions and resources; Go `/reload` only reloads frontend preferences. Restart Go backends after changing extension code. `npm ci` does not upgrade your global Pi, and unrelated packages from the author's root settings are not installed.

### Update notices and release notes

Startup checks Pi and user-level external packages in the background. Available updates appear below the default welcome content in the chat area: **notification only, with no popup or installation**. On the first launch after a Pi update, the same area shows the installed version's bundled release notes, with scrolling for longer entries. First use also shows them once; later launches do not repeat them. Extension release notes are not shown.

Failed checks and unchanged versions stay quiet. Offline mode skips network checks but still allows local Pi release notes. Only the frontend's separate cache records the displayed version; Pi settings and conversation history are unchanged. See [startup update notices](pi-goui/README_EN.md#updates-and-compatibility).

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

See the [pi-goui documentation](pi-goui/README_EN.md) for full usage and RPC boundaries.

### Codex Fast

This is an **optional third-party capability**, not a built-in Pi Go UI setting. Install `codex-fast` and `pi-ui` to use `/codex-fast` or the sidebar control. The menu selects one level: `normal`, `fast`, or `ultrafast`; Enter saves that level, Esc cancels the selection. The backend owns validation, legacy migration and atomic persistence to `codex-fast.json`.

The sidebar discovers the capability through Pi RPC, reads its current state and submits changes through the same service. No capability means no control. Go never reads or writes this extension's configuration, and no optimistic state is shown before backend confirmation.

Only `openai-codex` / `openai-codex-responses` requests are changed. `normal` adds no tier; `fast` sends `priority`; `ultrafast` sends `ultrafast` only for exact model ID `gpt-6-astra`, otherwise `priority`. This is a request policy, not a server-rejection retry or a guarantee of support/speed. The capability reloads persisted state before requests; manual file edits do not require a frontend-side configuration reader.

### Remaining Codex subscription quota

`pi-goui` shows remaining quota percentages, reset countdowns, and integer Credits in the sidebar instead of the old footer badge. Queries require the current model to use the official `openai-codex` origin and an existing usable OAuth login; custom origins and API keys do not trigger quota requests.

The frontend reads existing authentication and asynchronously queries the fixed `https://chatgpt.com/backend-api/wham/usage` endpoint roughly once a minute. It neither copies nor refreshes credentials, uses no proxy, and follows no redirects. Pi must handle expired login; unavailable fields use placeholders rather than fabricated quota.

### Other default behavior

These are source-code defaults, not copied local model or user settings; local configuration can override them.

- **Autoname** is enabled with notifications by default, using the current model, minimal reasoning, and a 600-second cooldown. Automatic naming runs after a primary (non-Worker) session fully settles, even without UI, and preserves manual names.
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

`pi-goui` renders real Worker details: objectives, status, model, session mode, usage, elapsed time, completion summaries, and recent activity, with task-level folding. Live tasks expand by default and rejoin tool groups on completion; manual folds take priority. It renders RPC data itself without loading the archived UI. Active TypeScript native rendering lives in `extensions/pi-ui/`; capabilities expose only structured data and operations.

Progress is truncated and filtered for common sensitive fields; this does not guarantee complete redaction. Cancellation, timeout, or output overflow triggers an attempt to terminate the subprocess; cancel calls wait for cleanup. Failures go to the main agent for handling and are not automatically retried. Reloading, switching sessions, or navigating the session tree within a Pi backend cleans up background tasks. Merely switching frontend tabs in `pi-goui` does not trigger that backend lifecycle; background sessions keep running. Historical cards use persisted snapshots on the current branch and never resume subprocesses.

`cwd` must remain inside the main working directory. `allowedPaths`/`forbiddenPaths` are checked only before `edit` and `write` tool calls. They are not a shell or filesystem sandbox and do not constrain `bash` or other extension tools; do not treat them as an isolation boundary. The main agent must inspect actual changes, pre-existing worktree modifications, verification evidence, and task scope before accepting the result.

Failure diagnostics retain termination sources and necessary process identifiers; they cannot establish why historical runs were cancelled. Custom SDK hosts must emit and await `session_shutdown`: bare `AgentSession.dispose()` in Pi 0.87.1 does not emit it. See the [Worker extension documentation](extensions/worker/README_EN.md) for lifecycle details, host limitations, and test commands.

## Configuration and security

- `settings.json` declares Pi extension packages and theme/display preferences. `worker-settings.json`, `codex-fast.json`, and `autoname.json` control their respective features. `keybindings.json` configures `Ctrl+Y` session resume; an optional user theme can live at `themes/pi.json`; that personal file is not bundled, and Go has embedded defaults.
- Keep login credentials and model secrets (such as `auth.json`, `models.json`, and `models-store.json`), sessions (such as `sessions/`) in ignored local files; do not commit or copy them into a public repository.
- Create custom model configuration locally and prefer environment-variable references for secrets; see [model configuration](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/models.md).
- `.gitignore` does not protect tracked files; check `git status --short --ignored` and `git diff --cached` before publishing. If credentials were exposed, remove the public content and rotate them.
- Telegram notifications are not used by default. To enable them, first review the extension source and securely configure the chat target and Bot Token; **never put credentials in the repository**. Notifications may send task input, replies, and questions to the target chat, and delivery is not guaranteed.
- Automatic session naming may send conversation content to its model, with extra requests and costs; check recipients before use.
- Tool-result filtering is heuristic and may miss secrets or mask ordinary content. It cannot replace credential management, pre-commit review, or restrictions on extensions' local permissions.

## Context management: SoL-Pi

[SoL-Pi](extensions/sol-pi/README_EN.md) loads automatically through `extensions/sol-pi/index.ts`; do not explicitly load its internal entries again. This local version includes only ObservationPack and Online Context Compact, not Action Fusion, Reducer, or the upstream global configuration loader.

- **ObservationPack**: plain-text, non-error tool results larger than 10 KiB remain full text for two provider requests, then become stable ID and head/tail references. The model can retrieve the originals in pages using `obs_recall`. Short text, errors, mixed media, and recall results are not packed; raw session history is not rewritten.
- **Online compaction**: runs only in persistent main sessions, not Workers. `update_plan` must submit the complete plan; completing a previously registered unfinished step while work remains can trigger compaction and continuation only after economic checks pass. Completing the whole plan does not trigger it. Summaries may lose detail and incur extra model costs; savings are not guaranteed. This is not the former `/roll-compact` manual-summary workflow. Tool calls are rendered by the active frontend, without relying on the old UI's hiding patches; there are no promotional banners or savings notices.
- **Local archives**: large results are stored under `sol-pi/<session-id>/observation-pack/` in the session directory. Non-persistent sessions use a temporary directory that survives exit, but mappings are not restored after a process restart. Treat these originals as sensitive data; they are not automatically deleted.

See [SoL-Pi validation](extensions/sol-pi/README_EN.md#offline-validation) for commands and version restrictions.

## Frontend validation

Run from `pi-goui/`:

```bash
go test -race ./...
go vet ./...
go build -trimpath -o bin/pi-goui ./cmd/pi-goui
python3 script/test-ui.py --binary bin/pi-goui
```

Default validation uses fake RPC and isolated PTYs, without paid model calls. Optional real-RPC checks run through a read-only allowlist proxy; see [pi-goui validation](pi-goui/README_EN.md#verification). See each extension's documentation for its validation commands and host-version constraints.
