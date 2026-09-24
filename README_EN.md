# Pi Coding Agent Configuration

[中文](README.md)

This is my personal [Pi Coding Agent](https://pi.dev) configuration: custom extensions, a theme, keybindings, and workflows, not a general-purpose distribution. This repository is for configuration source code safe to share. Extensions have local system access; review their code before running them, especially third-party extensions.

## Features

- Custom TUI: interface, dark theme, and `Ctrl+Y` session resume.
- `/context` shows context usage; `/usage` shows Codex usage.
- `ask_question` supports single-choice, multiple-choice, and multi-question confirmations; concurrent questionnaires are queued to avoid replacing each other. `/commit` generates a Conventional Commit message and commits (staging all changes).
- `/fast` attempts the priority tier for eligible Codex requests; thinking translation and automatic Chinese session naming are optional.
- `/impeccable` selects design instructions and inserts an editable prompt; this repository does not include the matching Skill.
- Indexed Memory retrieves local memories on demand; `/summarize` can request a summary. Memories are not distributed with the repository.
- [Worker](#worker) assists with scoped tasks in separate Pi subprocesses. Fast, Normal, and Deep can be selected automatically; **Max requires an explicit user request**. Path checks are not a security sandbox; inspect the resulting changes.
- SoL-Pi supports verification after file edits, recall of long results, and context compaction between plan steps. Tool-result filtering reduces exposure to common secrets but does not guarantee redaction.

## Installation

Requires Node.js **22.19+** and [Pi Coding Agent](https://github.com/earendil-works/pi). SoL-Pi OCC has been verified against Pi **0.87.1**; recheck compatibility after upgrading Pi.

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

Skip `mv` if `~/.pi/agent` does not exist. `npm ci` installs only local dependencies from the lockfile in `extensions/`. Once Pi starts, it separately manages the `pi-web-access` and `@ff-labs/pi-fff` extension packages declared in `settings.json`. Run `/login` on first use; run `/reload` after changing extensions, Skills, the theme, or keybindings.

## Common commands

| Command | Purpose |
| --- | --- |
| `/login`, `/model` | Configure authentication, choose a model |
| `/context`, `/usage` | Inspect context usage, Codex usage |
| `/summarize`, `/memory_settings` | Save reusable memory, adjust memory settings |
| `/worker_settings` | Adjust Worker models, concurrency, and other settings |
| `/commit` | Stage all changes, generate a commit message, and commit |
| `/fast`, `/thinking_translation` | Toggle the optional priority tier, thinking translation |
| `/impeccable`, `/reload` | Select a design instruction, reload configuration |

`/commit` sends the complete staged diff to the current model; ensure all working-tree changes belong in the same commit. `memory_search`, `memory_get`, `ask_question`, and `worker` are agent tools, not commands you need to run manually.

## Worker

`worker` delegates bounded, independently verifiable subtasks to separate Pi subprocesses. Workers do not persist sessions or automatically load Skills or Context Files, and may not create or call other Workers. The main agent decomposes the work, answers questions, inspects the actual diff, verifies it, and makes the final acceptance decision.

Starting, answering, continuing to wait, and cancelling all use the single `worker` tool. Each main session allows only one unfinished batch; put parallel tasks in the `tasks` array of a single call. Batch results retain input order and each task may complete, be blocked, or fail independently.

The summary and `changed_files` report Worker claims and workspace snapshot information; they do not replace the main agent's inspection of the actual diff.

Five modes are supported: `scout` for read-only investigation, `implement` for implementation, `test` for testing, `review` for read-only review, and `fix` for confirmed issues. Write modes must declare non-empty `allowedPaths`; `relevantFiles` are read hints and grant no write access.

### Tiers and settings

- **Fast**: clear, local work that is easy to verify.
- **Normal**: routine work and the default when unsure between Fast and Deep.
- **Deep**: difficult work such as cross-module changes, unknown root causes, or concurrent state; it is the highest automatically routed complexity tier.
- **Max**: not an automatic routing tier. Use it only when the user explicitly requests Max, maximum strength, or equivalent; never silently downgrade it.

Models and Thinking for every tier are read from the local `worker-settings.json`. Use `/worker_settings` to interactively change models, Thinking, concurrency, automatic delegation, timeout, and output limits; saved settings apply to subsequent tasks immediately. Configured models must be available.

### Questions and user confirmation

Workers can use `ask_parent` to ask the real main agent questions; no extra coordinator model or main-session fork is used. `worker` returns when a question needs an answer, otherwise waiting for the whole batch to finish without periodic polling returns. The main agent submits one or more answers through the same tool and continues waiting; independent tasks can keep running. Workers awaiting answers retain their concurrency slots and path locks.

When user judgment is needed, the main agent treats `ask_question` as a normal quick call, then explicitly relays the answer using the original question ID; user choices are never forwarded automatically. **Ordinary task and question timeouts keep running, including user confirmation and questionnaire queueing**; no extra time is reserved or added. Independent work continues. Late answers are rejected and cannot revive expired tasks or questions. Concurrent questionnaires display in order; cancellation and session shutdown clean up the UI and queue.

Cancelling or leaving a confirmation is not consent. Answers cannot expand the original task's permissions; broader scope requires cancellation, cleanup, and a new task. If a decision affects dangerous operations already running, cancel the relevant batch first: human waiting does not suspend arbitrary running tools.

### Parallelism, progress, and acceptance

Batch tasks are scheduled up to the concurrency limit: read-only tasks may run in parallel; read/write tasks conflict and must run serially; writers run in parallel only when `cwd` and all declared write paths can be resolved and proven disjoint. Unprovable independence is treated as a conflict; scheduling does not promise independent Git worktrees.

Each batch uses only its original Worker card, styled consistently with the tools in `extensions/ui/`. Follow-up answer, wait, and cancel calls remain in the model transcript but add no visible cards. Task status, tool activity, tier, turns, usage, elapsed time, completion summaries, and all per-task Q&A display in full on the original card by default. Batch-ID summary lines and routine execution-slot waiting notices are omitted; errors and timeout diagnostics remain visible.

Progress is truncated and filtered for common sensitive fields; this does not guarantee complete redaction. Cancellation, timeout, or output overflow triggers an attempt to terminate the subprocess; cancel calls wait for cleanup. Failures go to the main agent for handling and are not automatically retried. Reloading, switching sessions, or navigating the session tree cleans up background tasks. Historical cards use the latest persisted snapshot on the current branch and never resume subprocesses.

`cwd` must remain inside the main working directory. `allowedPaths`/`forbiddenPaths` are checked only before `edit` and `write` tool calls. They are not a shell or filesystem sandbox and do not constrain `bash`, other extension tools, or `then_run` commands; do not treat them as an isolation boundary. The main agent must inspect actual changes, pre-existing worktree modifications, verification evidence, and task scope before accepting the result.

For protocol details, limits, and test commands, see the [Worker extension documentation](extensions/worker/README.md).

## Configuration and security

- `settings.json` declares Pi extension packages. `worker-settings.json`, `memory-settings.json`, `fast.json`, `autoname.json`, and `thinking-translation-settings.json` control their respective features.
- Keep login credentials and model secrets (such as `auth.json`, `models.json`, and `models-store.json`), sessions and memories (such as `sessions/`, `memory-index.md`, and `memories/`) in ignored local files; do not commit or copy them into a public repository.
- Create custom model configuration locally and prefer environment-variable references for secrets; see [model configuration](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/models.md).
- `.gitignore` does not protect tracked files; check `git status --short --ignored` and `git diff --cached` before publishing. If credentials were exposed, remove the public content and rotate them.
- Telegram notifications are not used by default. To enable them, first review the extension source and securely configure the chat target and Bot Token; **never put credentials in the repository**. Notifications may send task input, replies, and questions to the target chat, and delivery is not guaranteed.
- Thinking translation and automatic session naming may send conversation content to their configured models; check the recipients before use.
- `/usage` relies on an undocumented Codex usage endpoint that may change. Tool-result filtering cannot replace credential management or restrict extensions' local permissions.

## SoL-Pi

`extensions/sol-pi/` provides three mechanisms:

1. **Action Fusion**: `edit`/`write` can run a verification command via `then_run` after a successful change. A failed command does not roll back the change, and the command is not sandboxed.
2. **ObservationPack**: replaces repeated large plain-text results with references that `obs_recall` can read back. Archives stay in local session directories and are neither automatically cleaned up nor redacted.
3. **Online Context Compact (OCC)**: evaluates compaction after eligible plan steps and continues work. Summaries can lose detail and incur extra model requests and cost; savings are not guaranteed. OCC targets Pi **0.87.1** and is disabled in Workers.

For provenance, settings, compatibility limitations, and offline test commands, see the [SoL-Pi extension documentation](extensions/sol-pi/README.md).
