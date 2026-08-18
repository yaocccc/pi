# Pi Coding Agent Configuration

[中文](README.md)

This is my personal [Pi Coding Agent](https://pi.dev) configuration, including custom extensions, themes, keybindings, and workflows. The repository is intended for public, reusable configuration source code. Credentials, model secrets, sessions, and personal memories must not be committed; provide them through environment variables or ignored local files.

## Features

- **Custom UI**: startup logo, editor, message cards, working status, and a compact footer.
- **Context inspection**: `/context` shows the current context-window breakdown and previews the System Prompt, Tools, Context Files, Skills, user/agent messages, and tool calls; detail views support Space for page-down navigation.
- **Codex usage**: `/usage` uses Pi's currently resolved OpenAI Codex authorization to show the subscription plan, primary, secondary, and additional usage windows, remaining allowance, reset times, and credits.
- **Structured questions**: the `ask_question` tool supports single choice, multiple choice, and custom input.
- **Plan workflow**: `/plan` creates a checklist, waits for confirmation, executes its steps continuously, and performs a final check.
- **Tiered memory**: automatically maintains user preferences, searches through `memory_search`, reads details through `memory_get`, lets the model queue end-of-turn persistence with `memory_summarize`, and retains manual `/summarize`.
- **Sensitive output filtering**: redacts common API keys, tokens, private keys, and connection strings before tool results enter the model context.
- **Fast mode**: `/fast` optionally enables the priority service tier for supported OpenAI Codex GPT-5.6 models; it is disabled by default.
- **Thinking translation**: translates short thinking content into Simplified Chinese and uses a persistent local cache to avoid duplicate translations.
- **Telegram notifications**: optionally sends task input and the final response to a configured chat.
- **Worker orchestration**: runs investigation, implementation, testing, and review in isolated Pi processes with model-tier routing and write-boundary checks.
- **Packages**: integrates `pi-web-access`, `@ff-labs/pi-fff`, and `context-mode`.
- **Theme and keybinding**: includes the custom dark `pi` theme and binds `Ctrl+Y` to the session resume picker.

## Structure

```text
.
├── extensions/                        # TypeScript extensions
│   ├── ask-question/                  # Structured user questions
│   ├── context/                       # /context usage and content previews
│   ├── fast/                          # Optional priority service tier
│   ├── filter-output/                 # Sensitive tool-result filtering
│   ├── memory/                        # Tiered memory, tools, and /summarize
│   ├── plan/                          # /plan workflow
│   ├── telegram/                      # Telegram task notifications
│   ├── thinking-translation/          # Chinese thinking translation and cache
│   ├── ui/                            # TUI customization
│   ├── usage/                         # /usage Codex subscription usage
│   ├── worker/                        # General-purpose Worker tool
│   │   └── agents/                    # Worker execution contract
│   └── herdr-agent-state.ts           # Herdr-managed extension state
├── skills/                            # Agent Skills and Worker orchestration
├── fast.json                          # Fast-mode toggle
├── memory-settings.json               # Memory summary and context settings
├── thinking-translation-settings.json # Thinking translation settings
├── worker-settings.json               # Worker models, concurrency, and limits
├── themes/pi.json                     # Custom theme
├── keybindings.json                   # Keybindings
└── settings.json                      # Global Pi settings
```

Local files such as `auth.json`, `models.json`, `models-store.json`, `sessions/`, `memory.md`, `memory-index.md`, and `memories/` are excluded by `.gitignore`. `.gitignore` does not protect files that are already tracked by Git.

## Installation

### Prerequisites

- Node.js 22+
- [Pi Coding Agent](https://github.com/earendil-works/pi)
- Optional: [fd](https://github.com/sharkdp/fd) for file completion in the `/plan` editor

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

Start Pi. Packages declared in `settings.json` are managed by Pi:

```bash
pi
```

Run `/login` to configure authentication. To use custom providers or models, recreate `~/.pi/agent/models.json` locally and prefer environment-variable references for secrets. This file is not tracked by Git. See [Custom Models](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/models.md).

To restore an existing local model configuration from the backup:

```bash
cp ~/.pi/agent.backup/models.json ~/.pi/agent/models.json
```

After changing extensions, skills, themes, or keybindings, run `/reload` in Pi.

## Commands

| Command | Description |
| --- | --- |
| `/context` | Show the context breakdown and preview prompts, tools, context files, skills, and message content |
| `/usage` | Show subscription usage, remaining allowance, and reset times for the current OpenAI Codex account |
| `/plan [task]` | Create a checklist, confirm it, execute it continuously, and run a final check |
| `/summarize` | Summarize the task and persist reusable indexed memory |
| `/memory_settings` | Interactively configure the memory limit, automatic summaries, model, thinking, notifications, and context sources |
| `/worker_settings` | Interactively configure Worker models, thinking, concurrency, automatic delegation, timeout, and output limits |
| `/fast` | Toggle the priority service tier for supported models; state is stored in `fast.json` |
| `/thinking_translation` | Toggle Simplified Chinese thinking translation |
| `/reload` | Reload extensions, skills, themes, and keybindings |
| `/login` | Configure provider authentication |
| `/model` | Select a model |
| `Ctrl+Y` | Open the session resume picker |

`memory_search`, `memory_get`, `memory_summarize`, `ask_question`, `plan_check_result`, and `worker` are agent tools and do not need to be invoked manually.

Use `/memory_settings` to edit the configuration interactively, or edit `memory-settings.json` directly. It configures the memory limit, automatic summaries, summary model, thinking level, result display, and inclusion of Tool Messages or thinking. `summarize.includeToolMessages` controls both Tool Calls and Tool Results; Memory Tool calls and results are always excluded. Subsequent summaries use the saved configuration immediately; after toggling automatic summaries, run `/reload` to synchronize `memory_summarize` tool registration. `summarize.resultDisplay` supports `message` (write to the conversation), `popup` (centered popup, default), and `none` (no notification). Missing files or fields use built-in defaults. `memory_get` compares the latest memory version with its most recent read in the active branch: unchanged content reuses the previous Tool Result, while updated content returns full fresh details. Context compaction or a branch summary permits a full reload. In TUI mode, summaries run in the background without blocking the editor or subsequent turns. Press `Esc` to cancel the model request; cancellation is disabled once writes begin to preserve index/detail consistency. The popup emphasizes results, memory titles, key fields, input/output context usage, and elapsed time.

## Optional Runtime Features

### Fast and Thinking Translation

`/fast` updates the toggle in `fast.json`. When enabled, it adds `service_tier: priority` only to requests for `gpt-5.6-sol`, `gpt-5.6-terra`, and `gpt-5.6-luna` on the `openai-codex` provider. The current configuration defaults to disabled.

`/thinking_translation` updates the toggle in `thinking-translation-settings.json`. The current configuration defaults to enabled, translates thinking content of at most 200 characters with the configured model, and caches results under `~/.pi/thinking-translations/`.

### Telegram Notifications

The Telegram extension only sends task notifications. It does not enable polling or remote replies. Configure the target Chat ID to use it:

```bash
export PI_TG_CHAT='<chat-id>'
```

The extension provides the Bot Token configuration. Notifications contain the project, session, current user input, and final response. Send failures are logged without interrupting the agent.

## Worker

`worker` runs tasks in isolated, sessionless Pi subprocesses. Fast, Normal, and Deep are automatic complexity tiers; Max is an execution-strength tier used only when explicitly requested by the user. Deep is the highest automatic tier. Full model IDs and thinking levels are configured in `worker-settings.json`.

Workers retain the normal tool set and load global extensions, while `PI_WORKER_DEPTH` prevents recursive delegation. Read-only tasks may run concurrently; batches containing writes run sequentially. Write tasks must declare allowed paths, and the subprocess validates `allowedPaths`, `forbiddenPaths`, and the working-directory boundary before `edit` or `write`. Use `/worker_settings` or edit `worker-settings.json` directly to configure models, thinking levels, concurrency, automatic delegation, timeout, and output limits. Saved settings apply to subsequent Worker tasks immediately.

The main Worker files are:

- `extensions/worker/index.ts`: tool entry point and orchestration; adjacent modules handle routing, processes, safety checks, and TUI behavior.
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

`/context` details are shown only in the local TUI and are not written to the session or sent to the model again. `/usage` does not read credential files directly: it uses Pi's resolved runtime authorization and sends only Bearer Authorization to the official `https://chatgpt.com` usage endpoint; custom or proxy origins are rejected.

When Telegram notifications are enabled, the project name, session name, current user input, and final response are sent to the target chat. When thinking translation is enabled, thinking content within the configured length limit is sent to the model selected in `thinking-translation-settings.json`, and translations are cached under `~/.pi/thinking-translations/`. Verify that these recipients and models satisfy your privacy requirements before enabling either feature.

Ignored data includes:

- Provider credentials and model API keys
- Session history and exported JSONL files
- User profiles, memory indexes, and project memory details
- npm/git packages installed by Pi
- Local dependencies, caches, logs, and temporary files

## Notes

This configuration is tailored to a personal workflow and is not a general Pi distribution. Extensions have full local system access; review all extension source code before using or modifying third-party extensions.

`/usage` relies on an undocumented ChatGPT usage endpoint whose fields or availability may change. It currently supports only the official OpenAI Codex origin.
