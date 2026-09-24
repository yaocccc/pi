# Pi Coding Agent Configuration

[中文](README.md)

This is my personal [Pi Coding Agent](https://pi.dev) configuration, including custom extensions, themes, keybindings, and workflows. The repository is intended to hold public, reusable configuration source code. Credentials, model secrets, session records, and personal memories should not be committed; provide them through environment variables or ignored local files.

## Features

- **Custom UI**: startup logo, editor, message cards, working status, and a compact footer.
- **Context inspection**: `/context` shows the current context-window breakdown and previews the System Prompt, Tools, Context Files, Skills, user/agent messages, and tool calls; detail views support Space for page-down navigation.
- **Codex usage**: `/usage` uses Pi's currently resolved OpenAI Codex authorization to show the subscription plan, primary, secondary, and additional usage windows, remaining allowance, reset times, and credits.
- **Structured questions**: `ask_question` supports single-question single or multiple choice and custom input, or collects several questions at once through `questions`, with step-by-step TUI navigation and a final confirmation.
- **Smart commits**: `/commit` stages all current changes, generates a concise English Conventional Commit message from the staged diff, and commits it.
- **Indexed Memory**: `memory_search` searches the index and `memory_get` reads details on demand; the model can request end-of-turn persistence of reusable knowledge through `memory_summarize`, or you can run `/summarize` manually.
- **Sensitive output filtering**: redacts common API keys, tokens, private keys, and connection strings before tool results enter the model context.
- **Fast mode**: `/fast` adds `service_tier: priority` to eligible OpenAI Codex GPT requests; availability of that tier still depends on provider and model support.
- **Thinking translation**: translates short thinking content into Simplified Chinese and uses a persistent local cache to avoid duplicate translations.
- **Automatic session naming**: `autoname` uses a configured model after an interactive agent turn has fully settled to decide whether to retain or update a concise Chinese session title.
- **Telegram notifications**: optionally sends structured questions, task input, and final responses to a specified chat.
- **Worker orchestration**: separate Pi subprocesses support tiered routing, conflict-aware concurrency, task progress and usage feedback, and `edit`/`write` path guards (not a sandbox).
- **SoL-Pi optimizations**: Action Fusion combines file changes and verification commands; ObservationPack replaces repeated large results with recallable references; Online Context Compact evaluates compaction at completed plan steps and then resumes work.
- **Impeccable design instructions**: `/impeccable` opens a searchable instruction picker or inserts an editable Skill prompt directly; this repository does not include the Impeccable Skill.
- **Packages**: `settings.json` declares `pi-web-access` and `@ff-labs/pi-fff`; it does not declare `context-mode`.
- **Theme and keybinding**: includes the custom dark `pi` theme and binds `Ctrl+Y` to the session resume picker.

## Structure

```text
.
├── extensions/                        # TypeScript extensions
│   ├── ask-question/                  # Structured user questions
│   ├── autoname/                      # Automatic session naming
│   ├── context/                       # /context usage and content previews
│   ├── commit/                        # /commit smart commits
│   ├── fast/                          # Optional priority service tier
│   ├── filter-output/                 # Sensitive tool-result filtering
│   ├── impeccable/                    # /impeccable instruction picker and prompt insertion
│   ├── memory/                        # Indexed memory, tools, and /summarize
│   ├── sol-pi/                        # Action Fusion, ObservationPack, OCC
│   ├── telegram/                      # Telegram task notifications
│   ├── thinking-translation/          # Chinese thinking translation and cache
│   ├── ui/                            # TUI customization
│   ├── usage/                         # /usage Codex subscription usage
│   ├── worker/                        # General-purpose Worker tool
│   │   └── agents/                    # Worker execution contract
│   └── herdr-agent-state.ts           # Optional Herdr state bridge file (not a herdr/ directory)
├── skills/                            # Two Skills: memory and worker-orchestration
├── autoname.json                      # Automatic session-naming configuration
├── fast.json                          # Fast-mode toggle
├── memory-settings.json               # Memory summary and result-display settings
├── thinking-translation-settings.json # Thinking translation settings
├── worker-settings.json               # Worker models, concurrency, and limits
├── themes/pi.json                     # Custom theme
├── keybindings.json                   # Keybindings
└── settings.json                      # Global Pi settings
```

Local files such as `auth.json`, `models.json`, `models-store.json`, `sessions/`, `memory.md`, `memory-index.md`, and `memories/` are excluded by `.gitignore`. `.gitignore` does not protect files already tracked by Git. `extensions/herdr-agent-state.ts` is a bridge file managed by external Herdr and enabled only when the requisite environment variables are present; this repository has no `herdr/` directory.

## Installation

### Prerequisites

- Node.js 22+
- [Pi Coding Agent](https://github.com/earendil-works/pi); the current SoL-Pi OCC has been verified against **0.87.1** and needs compatibility revalidation after upgrading Pi.

Install Pi:

```bash
npm install -g --ignore-scripts @earendil-works/pi-coding-agent
```

### Use this configuration

Back up an existing configuration, then clone this repository into Pi's global configuration directory:

```bash
mv ~/.pi/agent ~/.pi/agent.backup
git clone <repository-url> ~/.pi/agent
npm ci --prefix ~/.pi/agent/extensions
```

`npm ci --prefix` installs local extension dependencies (such as `node-telegram-bot-api` and `typebox`) in `extensions/` from `package-lock.json`; it is not the installation step for Pi-managed extension packages. After Pi starts, it separately manages the two npm extension packages declared in `settings.json`:

```bash
pi
```

Run `/login` on first use to configure authentication. To use custom providers or models, recreate `~/.pi/agent/models.json` locally and prefer environment-variable references for secrets. This file is not tracked by Git. See [Custom Models](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/models.md).

To restore an existing local model configuration from the backup:

```bash
cp ~/.pi/agent.backup/models.json ~/.pi/agent/models.json
```

After changing extensions, Skills, themes, or keybindings, run `/reload` in Pi.

## Commands

| Command | Description |
| --- | --- |
| `/context` | Show the context breakdown and preview the System Prompt, Tools, Context Files, Skills, and message content |
| `/usage` | Show subscription usage, remaining allowance, and reset times for the current OpenAI Codex account |
| `/commit` | Stage all changes, generate a Conventional Commit message from the staged diff, and commit |
| `/summarize` | Summarize the current task and persist reusable indexed memory |
| `/memory_settings` | Interactively configure the memory limit, automatic summaries, model, thinking, and notifications |
| `/worker_settings` | Interactively configure Worker models by tier, thinking, concurrency, automatic delegation, timeout, and output limits |
| `/impeccable`, `/impeccable <instruction>` | Select a design instruction in the interactive TUI, or insert that instruction's editable prompt into the editor (without executing it) |
| `/fast` | Toggle the priority service tier for supported models; state is stored in `fast.json` |
| `/thinking_translation` | Toggle Simplified Chinese thinking translation |
| `/autonameall` | One-time batch naming for every unnamed historical session that contains user text |
| `/reload` | Reload extensions, Skills, themes, and keybindings |
| `/login` | Configure provider authentication |
| `/model` | Select a model |
| `Ctrl+Y` | Open the session resume picker |

`memory_search`, `memory_get`, `memory_summarize`, `ask_question`, `worker`, `obs_recall`, and `update_plan` are agent tools and do not need to be invoked manually.

`/commit` first runs `git add -A`, sends the complete staged diff directly to the active model to generate a one-line English Conventional Commit message, and then runs `git commit`. It does not start an agent tool loop. Before running it, make sure every working-tree change belongs in the same commit.

## Indexed Memory

`skills/memory/SKILL.md` instructs the agent to search the index first and read details by name when needed. Repeating a `memory_search` query in the same session prompts reuse; `memory_get` compares the latest version against the most recently read version on the current branch, prompting reuse if unchanged and returning details only if updated. Full content can be read again after native compaction or a branch summary. If OCC compaction leaves a reuse prompt but the previous content is no longer visible, retrieve it through an explicit read path instead of relying on that prompt. The index and details live in the ignored `memory-index.md` and `memories/`, respectively, and are not distributed with the repository.

`/summarize` requests a manual summary. With automatic summaries enabled, the agent can also use `memory_summarize` to queue a summary after the current turn has fully settled. Summary context includes only user text and final assistant text, excluding thinking, tool calls, and tool results; the model may decide there is nothing of lasting value to save. `/memory_settings` or `memory-settings.json` configures the memory limit, automatic summaries, model, thinking level, and notification mode. Saved settings apply to subsequent summaries; run `/reload` after toggling automatic summaries to update tool registration. `summarize.resultDisplay` supports `message` (write to the conversation; the repository's current setting), `popup` (centered popup; code default), and `none` (no notification); missing or invalid configuration falls back to code defaults.

TUI summaries run in the background, allowing editing and further conversation. A progress widget shows stage, model, token estimates and usage, and elapsed time. Press `Esc` to cancel while the model request is running; cancellation is disabled once actual writes begin. In `popup` mode, the scrollable result shows processing status, memory titles and fields, usage, and duration, with keyboard or mouse scrolling; `message` mode displays the result in the conversation.

## SoL-Pi Context and Execution Optimizations

`extensions/sol-pi/` is adapted from [NVlabs/SoL-Pi](https://github.com/NVlabs/SoL-Pi), retains the MIT license, and is auto-discovered by Pi. The following three mechanisms are enabled; **Evidence-Preserving Reducer is not included**, and the upstream `sol-pi.json` configuration is not read.

### Action Fusion

`edit` and `write` retain their native parameters and add optional `then_run`, for example `{"command":"npm test","timeout":30}`. The command runs only after the file change succeeds and returns its result in the same tool call. A command failure does not roll back the edit. Without `then_run`, native behavior remains unchanged.

The fused command invokes the shell internally and does not emit a separate `bash` tool-call event, so it does not automatically pass through tool-call guards that target only `bash`. This is not a sandbox; command side effects must still stay within the task's scope.

### ObservationPack

Non-error, plain-text tool results larger than **10 KiB** are sent in full for the first two model requests, then replaced with references containing excerpts from the beginning and end. The agent can page through the original by byte offset using `obs_recall`; short, error, and mixed-media results are not packed.

This changes only the context projection sent to the model, not the original session history. Original content and JSONL records are stored under `sol-pi/<session-id>/observation-pack/` in the corresponding session directory and are not automatically cleaned up; deleting the archive prevents later recall. Neither the archive nor its excerpts guarantee redaction.

### Online Context Compact (OCC)

The agent uses `update_plan` to maintain the complete task plan. OCC considers compaction only when the plan was successfully updated in the current turn, a step was just completed, and unfinished work remains; not every update triggers it. Estimates consider remaining main-agent turns, context growth, cache-rebuild cost, and window pressure. The default cache write/read cost ratio of **12.5** is only a heuristic, not a claim about actual billing savings.

When conditions are met, OCC uses the current model to generate a native-format summary, commits compaction and a reminder of the remaining plan at a turn boundary in Pi **0.87.1**, and then continues naturally. Cancellation, failure, or an invalid summary does not commit compaction or start an additional continuation. A compaction may add one or two summary requests plus retry costs, and the summary may still lose details.

OCC respects existing `compaction.enabled`, `keepRecentTokens`, and `reserveTokens` settings; it is disabled without a persistent session, and Workers with `PI_WORKER_DEPTH > 0` do not register its tool or events. It does not restart tasks through `agent_settled`, but its boundary compaction does not fire the native `session_before_compact` / `session_compact` hooks. Compatibility with other extensions that depend on those events must be evaluated separately.

### Verification and Maintenance

```bash
node extensions/sol-pi/tests/run-global.mjs
node extensions/sol-pi/tests/run-global.mjs --typecheck
```

**57 offline tests** and strict type checking have passed, covering repeated compaction, cancellation, failure, cache warm-up isolation under a real Pi 0.87.1 AgentSession, and regression tests for the other two mechanisms. Tests use temporary directories and installed global dependencies; they neither call remote models nor modify dependencies or lockfiles. Type checking requires an existing global TypeScript compiler.

Run `/reload` after changes or start a new session. To uninstall, remove only `extensions/sol-pi/` and reload; historical archives can be cleaned up separately. For provenance, SDK settings injection, and detailed compatibility constraints, see the [SoL-Pi extension notes](extensions/sol-pi/README.md).

## Optional Runtime Features

### Fast and Thinking Translation

`/fast` updates the toggle in `fast.json`. When enabled, it adds `service_tier: priority` only if the provider is `openai-codex`, the API is `openai-codex-responses`, and the model ID begins with `gpt`; it is no longer limited to three GPT-5.6 IDs. The code default is disabled, while `enabled` is `true` in the repository's current `fast.json`. The server decides whether priority service is actually available. Worker processes load this extension too.

`/thinking_translation` updates the toggle in `thinking-translation-settings.json`. The current configuration defaults to enabled, translates thinking content of at most 200 characters with the configured model, and caches results under `~/.pi/thinking-translations/`.

### Impeccable Design Instructions

`/impeccable` opens a searchable design-instruction picker in the TUI; `/impeccable shape` and `/impeccable hooks status`, for example, directly select exact instructions from the catalog. The extension only inserts a `/skill:impeccable <instruction>` template into the main editor for the user to complete with a target, review, and submit; it does not itself perform design operations. `extensions/impeccable/commands.ts` lists the available instructions. This repository's `skills/` contains only memory and worker-orchestration, not an Impeccable Skill. Install a discoverable Skill of that name before trying to execute the template; otherwise it cannot carry out the corresponding design workflow.

### Automatic Session Naming

`autoname` runs only in sessions with a UI and checks whether naming is needed after every `agent_settled` event. It sends only user text, assistant text, and the existing session name from the active branch to the naming model; thinking blocks, tool calls, tool results, Skills, and Pi's system prompt are excluded. User-assigned names are never overwritten. For an unnamed session or a title previously generated by the extension, the model must return either `keep` or `rename`, and any new title must be concise and written in Chinese.

The repository's current configuration is stored in `autoname.json`:

```json
{
  "enabled": true,
  "notify": true,
  "cooldownSeconds": 600,
  "model": "openai-codex/gpt-5.3-codex-spark",
  "reasoning": "minimal"
}
```

`enabled` is the feature-wide toggle, while `notify` controls rename notifications. `cooldownSeconds` is expressed in seconds and defaults to 600; set it to `0` to disable cooldown. Whenever a naming request is actually started, the extension stores its timestamp as a Custom Entry in the current session, so `/reload` and resumed sessions continue to enforce the cooldown on the active branch. `model` accepts a full `provider/model` value or `auto` to reuse the main session model. The built-in default is `auto`; the repository's current configuration explicitly selects `openai-codex/gpt-5.3-codex-spark`. `reasoning` selects the reasoning effort for the naming request. The file is re-read on every check, so edits do not require `/reload`. When a name changes, the notification includes the naming request's context-token usage and elapsed time.

`/autonameall` is a one-time historical backfill command. It scans sessions from every project, skips records that already have a name or contain no user text, and sequentially uses the model and reasoning effort in `autoname.json` to force a Chinese name for every remaining session. This explicit command is not gated by the automatic-naming toggle or cooldown, and it never changes an existing name. Each actual request still records a cooldown timestamp so that resuming the session does not immediately trigger another automatic naming request. At completion, the command reports renamed, skipped, and failed counts plus total elapsed time.

### Telegram Notifications

The Telegram extension only sends task notifications; it does not enable polling or remote replies. The target environment variable currently read by the source is **`PI_TG_CHAT`**. The Bot Token is currently provided by a local value in `extensions/telegram/index.ts`; **`PI_TG_TOKEN` is not a configuration setting read by the current implementation**. Do not commit or publish real tokens. Review this and switch to secure injection before deployment; this README contains no credentials. No notification is sent without a Token or Chat ID.

In sessions with a UI, `agent_end` may send the project, session, current user input, and final text response. A single `ask_question` question with `question`/`options` sends an awaiting-reply notification (a multi-question `questions` call does not send notifications for each question). Send exceptions are currently silently ignored: delivery is not guaranteed, and answers cannot be relayed back from Telegram.

## Worker

`worker` runs `scout`, `implement`, `test`, `review`, or `fix` tasks in separate, sessionless Pi subprocesses (`--mode json --print --no-session --no-skills --no-context-files`). Fast, Normal, and Deep may be selected automatically; Max is used only on explicit user request, and Deep is the highest automatic tier. Models and thinking levels for each tier are configured in `worker-settings.json`; configured models must be available. Workers do not explicitly restrict the tool list and load global extensions; `PI_WORKER_DEPTH` prevents recursive delegation, while SoL-Pi OCC is disabled in Workers.

Each task's TUI Worker card shows queued/running/completed status, tier, turns, usage, elapsed time, recent tools and a thinking summary, and the final conclusion. The working indicator aggregates main-session and Worker input/output tokens. Subprocesses report stages, tool execution, and usage through JSON events; streaming usage includes estimates. UI summaries are truncated and filtered for common sensitive fields, **not fully redacted or security-isolated**. On cancellation, timeout, or output overflow, termination of the subprocess is attempted; failures are not automatically retried, and the main agent must still inspect the actual diff.

Batch scheduling preserves input result order within `maxConcurrentWorkers`: read-only tasks may run together; reads and writes are mutually exclusive; writers run together only when every `task.cwd` and `allowedPaths` can be normalized to the main workspace and proven disjoint. A path without a glob is exact even when it currently names a directory; use `dir/**` for descendants. Identical paths, ancestor/descendant path nodes, overlapping explicit subtrees or static glob prefixes, symlink aliases, root-level or dangling symlinks, and statically unresolvable globs conservatively conflict. The scheduler may scan past a blocked queue head to start a later independent task. Write tasks must declare allowed paths. The Worker subprocess checks `allowedPaths`, `forbiddenPaths`, and the working-directory boundary **only before `edit` and `write` tool calls; it does not intercept `bash`, other extension tools, or `then_run` commands** and is not a filesystem sandbox. Only tasks that actually overlap a sibling writer intersect `changed_files` with their canonical declaration; serial tasks retain the full raw delta. Every result also exposes the complete snapshot observation as `observed_changed_files`. Because writers share one Git worktree, the source of paths outside a task's declaration cannot be proven and is called out in `risks`. Use `/worker_settings` or edit `worker-settings.json` directly to configure models, thinking levels, concurrency, automatic delegation, timeout, and output limits. Saved settings apply to subsequent Worker tasks immediately.

The main Worker files are:

- `extensions/worker/index.ts`: tool entry point and orchestration; `process.ts`, `ui.ts`, `guard.ts`, and other nearby modules handle subprocess events, TUI behavior, and `edit`/`write` guards.
- `skills/worker-orchestration/SKILL.md`: main-agent rules for decomposition, delegation, review, and acceptance.
- `extensions/worker/agents/worker.md`: Worker execution rules and structured result format.
- `worker-settings.json`: models, thinking levels, concurrency, timeout, and final-output limit.

## Privacy and Security

Always inspect staged changes before publishing:

```bash
git status --short --ignored
git diff --cached
```

`.gitignore` only protects files that are not already tracked. If a sensitive file has ever been committed, remove it from the Git index and history, then rotate the affected credentials immediately. `filter-output` only redacts tool results entering the model context; it is not a substitute for credential management and does not prevent other extensions from sending data.

`/context` details are shown only in the local TUI and are not written to the session or sent to the model again. `/usage` does not read credential files directly: it uses Pi's resolved runtime authorization and sends Bearer Authorization only to the official `https://chatgpt.com` usage endpoint; custom or proxy origins are rejected. When `/commit` runs, the current staged diff is sent to the selected model to generate the commit message.

When Telegram notifications are enabled, the project name, session name, current user input, final response, and the single-question `ask_question` question and options may be sent to the target chat. The current Token comes from extension source rather than `PI_TG_TOKEN`; remove any real credentials and rotate exposed tokens before publishing or sharing. When thinking translation is enabled, thinking content within the configured length limit is sent to the model selected in `thinking-translation-settings.json`, and translations are cached under `~/.pi/thinking-translations/`. When automatic session naming is enabled, user text, assistant text, and the existing session name from the active branch are sent to the model selected in `autoname.json`; running `/autonameall` expands that scope to every eligible unnamed historical session across all projects. Cooldown records remain local Custom Entries in their corresponding sessions and do not enter model context. Verify that these recipients and models satisfy your privacy requirements before enabling these features.

Ignored data includes:

- Provider credentials and model API keys
- Session history and exported JSONL files
- User profiles, memory indexes, and project memory details
- npm/git packages installed by Pi
- Local dependencies, caches, logs, and temporary files

## Notes

This configuration is tailored to a personal workflow and is not a general Pi distribution. Extensions have full local system access; review all extension source code before using or modifying third-party extensions.

`/usage` relies on an undocumented ChatGPT usage endpoint whose fields or availability may change. It currently supports only the official OpenAI Codex origin.
