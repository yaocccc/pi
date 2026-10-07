# Pi Coding Agent Configuration

[中文](README.md)

This is my personal [Pi Coding Agent](https://pi.dev) configuration: custom extensions, a theme, keybindings, and workflows, not a general-purpose distribution. This repository is for configuration source code safe to share. Extensions have local system access; review their code before running them, especially third-party extensions.

## Features

- Custom TUI: interface, dark theme, and `Ctrl+Y` session resume; the footer shows context usage, model, Thinking, and Codex Fast switches.
- `ask_question` supports single-choice, multiple-choice, and multi-question confirmations; concurrent questionnaires are queued to avoid replacing each other. `/commit` generates a Conventional Commit message and commits (staging all changes).
- `/codex-fast` separately configures Fast / Ultrafast request tiers; Chinese thinking translation and automatic Chinese session naming are enabled by default and can be disabled in their configuration.
- [Worker](#worker) assists with scoped tasks in separate Pi subprocesses. Automatic routing has only three tiers: Fast, Normal, and Deep; Thinking strength is configured independently. Path checks are not a security sandbox; inspect the resulting changes.
- [SoL-Pi](#context-management-sol-pi) archives large tool results and provides online context compaction. Tool-result filtering reduces exposure to common secrets but does not guarantee redaction.

## Local extension inventory

Each row corresponds to current source under `extensions/`, excluding `node_modules`, tests, and helper modules. Pi discovers top-level `.ts` files and directories containing `index.ts`. `ask-parent.ts` is explicitly loaded only in Worker subprocesses. Do not load these internal entries a second time.

| Extension / source | Purpose | Entry point | Configuration / activation |
| --- | --- | --- | --- |
| [ask-question](extensions/ask-question/index.ts) | Queued single-choice, multiple-choice, custom-input, or multi-question forms | Agent tool `ask_question`, not a slash command | No separate configuration; interactive forms require TUI, other modes return unanswered questions only |
| [autoname](extensions/autoname/index.ts) | Generate/update Chinese session names while preserving manual names | Automatic after the run fully settles | `autoname.json`: `enabled`, `notify`, `cooldownSeconds`, `model`, `reasoning` |
| [btw](extensions/btw/index.ts) | Temporary read-only Q&A from a snapshot of the current context, without tools or writes to the main session | `/btw [question]`, `Ctrl+B` | Requires TUI; uses the current model and authentication; closing discards Q&A but API usage is still billed |
| [codex-fast](extensions/codex-fast/index.ts) | Set Fast / Ultrafast for eligible Codex requests | Only the `/codex-fast` native selection menu | `codex-fast.json`: independent boolean switches `fast`, `ultrafast`, both off by default |
| [commit](extensions/commit/index.ts) | Stage all changes, generate an English Conventional Commit with the current model, and commit immediately | `/commit` | No separate configuration; requires Git, an available model and authentication; no pre-commit confirmation menu |
| [filter-output](extensions/filter-output/index.ts) | Filter common sensitive text and certain sensitive-file reads before successful tool results reach the model | Automatic `tool_result` hook, not a user command | No separate configuration; redaction is not guaranteed; errors are not filtered and `.env.example` reads bypass filtering |
| [herdr-agent-state](extensions/herdr-agent-state.ts) | Report working/blocked/idle state and session references to Herdr's local socket | **Internal bridge, not a user command** | `HERDR_ENV=1` with `HERDR_SOCKET_PATH` and `HERDR_PANE_ID`; binds only to the main session with UI; managed/overwritten by Herdr |
| [sol-pi](extensions/sol-pi/index.ts) | ObservationPack archives large tool results; Online Context Compact compacts context online | Automatic context hooks; agent tools `obs_recall`, `update_plan` | Auto-discovered entry; do not load twice. See [SoL-Pi docs](extensions/sol-pi/README.md) |
| [telegram](extensions/telegram/index.ts) | Send run replies and some question notifications to a target chat | Automatic event notifications, not a user command; no remote control | Environment variables `PI_TG_TOKEN`, `PI_TG_CHAT`; sends nothing if either is missing; no polling; depends on `node-telegram-bot-api` |
| [thinking-translation](extensions/thinking-translation/index.ts) | Add Chinese display translations to short Thinking without changing source sessions or model context | `/thinking_translation` toggle; automatically triggered by live streaming | `thinking-translation-settings.json`: `enabled`, `model`, `maxThinkingLength`; requests always use minimal reasoning; local cache at `~/.pi/thinking-translations/` |
| [ui](extensions/ui/index.ts) | Custom header, editor, footer, tool cards, and main-agent + Worker progress/usage | Automatic TUI rendering, not a user command | No separate configuration; footer reads `codex-fast.json`; theme/display settings in `settings.json`, shortcuts in `keybindings.json` |
| [worker](extensions/worker/index.ts) | Bounded subprocess tasks, parallel scheduling, and progress cards | Agent tool `worker`; `/worker_settings` | `worker-settings.json`: required three-tier models/Thinking, concurrency, automatic delegation, timeout, and output limit; in subprocesses, this entry installs only the write guard, not nested delegation |
| [Worker / ask-parent](extensions/worker/ask-parent.ts) | Let a Worker ask the real main agent for a decision | **Internal bridge tool `ask_parent`, not a user command** | Explicitly loaded by Worker; requires subprocess depth and the fd 3 control channel; default answer timeout 120 seconds, configurable to 1–600 seconds |

The old local `context`, `usage`, and `fast` extensions have been removed; their `/context`, `/usage`, and `/fast` commands are no longer provided by these extensions. Context usage appears in the footer; Fast settings use `/codex-fast`. `rolling-compact` and its configuration have been removed, and `/roll-compact` is no longer provided; context management uses SoL-Pi again. External packages are not part of this local inventory.

## Installation

Requires Node.js **22.19+** and [Pi Coding Agent](https://github.com/earendil-works/pi), with Git for commits and Worker change checks.

```bash
npm install -g --ignore-scripts @earendil-works/pi-coding-agent
```

Back up your existing personal agent directory first (if present). Ensure the backup destination does not exist to avoid overwriting it. Replace `<repository-url>` with this repository's URL:

```bash
mv ~/.pi/agent ~/.pi/agent.backup
git clone <repository-url> ~/.pi/agent
npm ci --prefix ~/.pi/agent/extensions
```

Start Pi:

```bash
pi
```

Skip `mv` if `~/.pi/agent` does not exist. `npm ci` installs local dependencies from the lockfile in `extensions/` (runtime dependencies `typebox` and `node-telegram-bot-api`, plus Pi SDK / TypeScript development dependencies); it does not upgrade global Pi. Once Pi starts, it separately manages the `pi-web-access` extension package declared in `settings.json`. Run `/login` on first use; run `/reload` after changing extensions, Skills, the theme, or keybindings. Local SDK development dependencies in the lockfile do not replace the validated global host version. SoL-Pi's audit and test runner currently target Pi **1.0.2**; the global Pi inspected here is **1.0.4**, so its version gate stops the tests before execution. Compatibility with that version remains unverified.

## Common commands

| Command | Purpose |
| --- | --- |
| `/login`, `/model` | Configure authentication, choose a model |
| `/worker_settings` | Adjust Worker models, concurrency, and other settings |
| `/commit` | Stage all changes, generate a commit message, and commit |
| `/btw [question]` | Open temporary read-only Q&A (also available via `Ctrl+B`) |
| `/codex-fast` | Open the Fast / Ultrafast settings menu |
| `/thinking_translation` | Toggle Chinese thinking translation |
| `/reload` | Reload configuration |

`/login`, `/model`, and `/reload` are Pi built-in commands; the others come from the local extensions above. `/commit` sends the complete staged diff to the current model; ensure all working-tree changes belong in the same commit. `ask_question`, `worker`, `obs_recall`, and `update_plan` are agent tools, not slash commands; optional tools depend on their activation conditions.

### Codex Fast

`/codex-fast` only opens the native selection menu; it does not accept subcommands such as `on` / `off`. Select Fast or Ultrafast with the arrow keys. **Enter toggles that switch and saves immediately** to `codex-fast.json`, keeping the menu open. **Esc exits** without undoing saved toggles. Menu saves take effect immediately; use `/reload` after editing the configuration file manually.

Requests are modified only when the provider is `openai-codex` and the API is `openai-codex-responses`. If Ultrafast is enabled and the ID **exactly equals `gpt-6-astra`**, the payload uses `service_tier: "ultrafast"`; otherwise it falls back to `"priority"` only if Fast is enabled. Ultrafast alone leaves requests unchanged for nonmatching models. This is not a retry/downgrade after server rejection and guarantees neither server support nor speed.

In the footer, **✨ means the Fast switch is on, and 🌟 means the Ultrafast switch is on**. These independent markers can appear together. They show saved switch states, not which request tier the current model actually uses.

### Other default behavior

These are source-code defaults, not copied local model or user settings; local configuration can override them.

- **Autoname** is enabled with notifications by default, using the current model, minimal reasoning, and a 600-second cooldown. Automatic naming runs only after a session with UI fully settles and preserves manual names.
- **Thinking translation** is enabled by default with a default 200-character limit and changes display only. A missing configuration file or unavailable configured model falls back to the current session model. History restoration reads existing cache only, never requesting missing historical translations; reload after changing model/length configuration.
- **Telegram** sends nothing without both environment variables; it only notifies and does not receive chat commands. The current question watcher recognizes only the top-level `question` in `ask_question`; multi-question `questions` forms do not produce matching question notifications. Failures are silently ignored; delivery is not guaranteed.

## Worker

`worker` delegates bounded, independently verifiable subtasks to separate Pi subprocesses. Workers do not persist sessions or automatically load Skills or Context Files, and may not create or call other Workers. The main agent decomposes the work, answers questions, inspects the actual diff, verifies it, and makes the final acceptance decision.

Starting, answering, continuing to wait, and cancelling all use the single `worker` tool. Each main session allows only one unfinished batch; put parallel tasks in the `tasks` array of a single call. Batch results retain input order and each task may complete, be blocked, or fail independently.

The summary and `changed_files` report Worker claims and workspace snapshot information; they do not replace the main agent's inspection of the actual diff.

Five modes are supported: `scout` for read-only investigation, `implement` for implementation, `test` for testing, `review` for read-only review, and `fix` for confirmed issues. Write modes must declare non-empty `allowedPaths`; `relevantFiles` are read hints and grant no write access. `allowedPaths` and `forbiddenPaths` are relative to `task.cwd`: `backend/file.ts` matches exactly one file (including a new file), while directory descendants require `backend/**`. Existing bare directories such as `backend`, `./backend`, or `backend/` fail validation before any batch task starts, with a `backend/**` suggestion. Use `backend/**` even for directories not yet created; a nonexistent bare path is not assumed to be a directory. Use `/**` to exclude directory descendants in `forbiddenPaths` too.

### Tiers and settings

- **Fast**: clear, local work that is easy to verify.
- **Normal**: routine work and the default when unsure between Fast and Deep.
- **Deep**: difficult work such as cross-module changes, unknown root causes, or concurrent state; it is the highest task-complexity tier.

`preset` accepts only `auto`, `fast`, `normal`, and `deep`; **there is no Max execution tier**. Thinking levels `high`, `xhigh`, and `max` are independent per-tier reasoning strengths, not extra routing tiers, and must be supported by the model.

Models and Thinking for every tier are read from local `worker-settings.json`; `fast`, `normal`, and `deep` must each specify a valid model. Missing/unavailable models produce errors, never guessed routes or silent downgrades. Loading legacy configuration removes and persists the obsolete top-level `max` entry. General source defaults are concurrency 3, automatic delegation enabled, 15-minute task timeout, and 64 KiB output limit. `/worker_settings` interactively edits these settings and per-tier models/Thinking; choosing “Save and exit” applies them immediately to subsequent tasks. When automatic delegation is off, explicit starts require `manual: true`.

### Questions and user confirmation

Workers can use `ask_parent` to ask the real main agent questions; no extra coordinator model or main-session fork is used. `worker` returns when a question needs an answer, otherwise waiting for the whole batch to finish without periodic polling returns. The main agent submits one or more answers through the same tool and continues waiting; independent tasks can keep running. Workers awaiting answers retain their concurrency slots and path locks.

When user judgment is needed, the main agent treats `ask_question` as a normal quick call, then explicitly relays the answer using the original question ID; user choices are never forwarded automatically. **Ordinary task and question timeouts keep running, including user confirmation and questionnaire queueing**; no extra time is reserved or added. Independent work continues. Late answers are rejected and cannot revive expired tasks or questions. Concurrent questionnaires display in order; cancellation and session shutdown clean up the UI and queue.

Cancelling or leaving a confirmation is not consent. Answers cannot expand the original task's permissions; broader scope requires cancellation, cleanup, and a new task. If a decision affects dangerous operations already running, cancel the relevant batch first: human waiting does not suspend arbitrary running tools.

### Parallelism, progress, and acceptance

Batch tasks are scheduled up to the concurrency limit: read-only tasks may run in parallel; read/write tasks conflict and must run serially; writers run in parallel only when `cwd` and all declared write paths can be resolved and proven disjoint. Unprovable independence is treated as a conflict; scheduling does not promise independent Git worktrees. Sessions in the same process own separate children, slots, and cleanup. Separate Pi windows have isolated control channels but may still share a worktree; path locks do not span windows.

Each batch uses only its original Worker card, styled consistently with the tools in `extensions/ui/`. Follow-up answer, wait, and cancel calls remain in the model transcript but add no visible cards. Task status, tool activity, tier, turns, usage, elapsed time, completion summaries, and all per-task Q&A display in full on the original card by default. Batch-ID summary lines and routine execution-slot waiting notices are omitted; errors and timeout diagnostics remain visible.

Progress is truncated and filtered for common sensitive fields; this does not guarantee complete redaction. Cancellation, timeout, or output overflow triggers an attempt to terminate the subprocess; cancel calls wait for cleanup. Failures go to the main agent for handling and are not automatically retried. Reloading, switching sessions, or navigating the session tree cleans up background tasks. Historical cards use the latest persisted snapshot on the current branch and never resume subprocesses.

`cwd` must remain inside the main working directory. `allowedPaths`/`forbiddenPaths` are checked only before `edit` and `write` tool calls. They are not a shell or filesystem sandbox and do not constrain `bash` or other extension tools; do not treat them as an isolation boundary. The main agent must inspect actual changes, pre-existing worktree modifications, verification evidence, and task scope before accepting the result.

Unexpected disconnection during a pending question fails and cleans up the worker, even if it catches the IPC error and claims success. There is no automatic reconnect or redispatch. Failure diagnostics retain termination sources and necessary process identifiers; they cannot establish why historical runs were cancelled. Custom SDK hosts must emit and await `session_shutdown`: bare `AgentSession.dispose()` in Pi 0.87.1 does not emit it. See the [Worker extension documentation](extensions/worker/README.md) for protocol details, host limitations, and test commands.

## Configuration and security

- `settings.json` declares Pi extension packages and theme/display preferences. `worker-settings.json`, `codex-fast.json`, `autoname.json`, and `thinking-translation-settings.json` control their respective features. `keybindings.json` configures `Ctrl+Y` session resume; theme source is [themes/pi.json](themes/pi.json).
- Keep login credentials and model secrets (such as `auth.json`, `models.json`, and `models-store.json`), sessions (such as `sessions/`) in ignored local files; do not commit or copy them into a public repository.
- Create custom model configuration locally and prefer environment-variable references for secrets; see [model configuration](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/models.md).
- `.gitignore` does not protect tracked files; check `git status --short --ignored` and `git diff --cached` before publishing. If credentials were exposed, remove the public content and rotate them.
- Telegram notifications are not used by default. To enable them, first review the extension source and securely configure the chat target and Bot Token; **never put credentials in the repository**. Notifications may send task input, replies, and questions to the target chat, and delivery is not guaranteed.
- Thinking translation and automatic session naming may send Thinking or conversation content to their respective models, with extra requests and costs; check recipients before use. Treat local translation caches and Herdr session references as sensitive data too.
- Tool-result filtering is heuristic and may miss secrets or mask ordinary content. It cannot replace credential management, pre-commit review, or restrictions on extensions' local permissions.

## Context management: SoL-Pi

[SoL-Pi](extensions/sol-pi/README.md) loads automatically through `extensions/sol-pi/index.ts`; do not explicitly load its internal entries again. This local version includes only ObservationPack and Online Context Compact, not Action Fusion, Reducer, or the upstream global configuration loader.

- **ObservationPack**: plain-text, non-error tool results larger than 10 KiB remain full text for two provider requests, then become stable ID and head/tail references. The model can retrieve the originals in pages using `obs_recall`. Short text, errors, mixed media, and recall results are not packed; raw session history is not rewritten.
- **Online compaction**: runs only in persistent main sessions, not Workers. `update_plan` must submit the complete plan; completing a previously registered unfinished step while work remains can trigger compaction and continuation only after economic checks pass. Completing the whole plan does not trigger it. Summaries may lose detail and incur extra model costs; savings are not guaranteed. This is not the former `/roll-compact` manual-summary workflow. Tool calls are hidden, with no promotional banners or savings notices.
- **Local archives**: large results are stored under `sol-pi/<session-id>/observation-pack/` in the session directory. Non-persistent sessions use a temporary directory that survives exit, but mappings are not restored after a process restart. Treat these originals as sensitive data; they are not automatically deleted.

See [SoL-Pi validation](extensions/sol-pi/README.md#offline-validation) for commands and version restrictions.
