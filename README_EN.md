# Pi Coding Agent Configuration

[中文](README.md)

This is my personal [Pi Coding Agent](https://pi.dev) configuration, including custom extensions, themes, keybindings, and workflows. The repository only stores public and reusable configuration source code. Credentials, model secrets, sessions, and personal memories are excluded by `.gitignore`.

## Features

- **Custom UI**: startup logo, editor, message cards, working status, and a compact footer.
- **Context inspection**: `/context` shows the current context-window breakdown and previews the System Prompt, Tools, Context Files, and Skills; detail views support Space for page-down navigation.
- **Codex usage**: `/usage` uses Pi's currently resolved OpenAI Codex authorization to show the subscription plan, primary, secondary, and additional usage windows, remaining allowance, reset times, and credits.
- **Structured questions**: the `ask_question` tool supports single choice, multiple choice, and custom input.
- **Plan workflow**: `/plan` creates a checklist, waits for confirmation, executes its steps continuously, and performs a final check.
- **Tiered memory**: automatically maintains user preferences, searches through `memory_search`, reads details through `memory_get`, lets the model queue end-of-turn persistence with `memory_summarize`, and retains manual `/summarize`.
- **Sensitive output filtering**: redacts common API keys, tokens, private keys, and connection strings before tool results enter the model context.
- **Request optimization**: enables the priority service tier for supported OpenAI Codex GPT-5.6 models.
- **Packages**: integrates `pi-web-access` and `@ff-labs/pi-fff`.
- **Theme and keybinding**: includes the custom dark `pi` theme and binds `Ctrl+Y` to the session resume picker.

## Structure

```text
.
├── extensions/          # TypeScript extensions
│   ├── ask-question/    # Structured user questions
│   ├── context/         # /context usage breakdown and content previews
│   ├── fast/            # Model request optimization
│   ├── filter-output/   # Sensitive output filtering
│   ├── memory/          # Tiered memory, memory tools, and /summarize
│   ├── plan/            # /plan workflow
│   ├── subagents/       # Chinese Subagents workflows
│   ├── usage/           # /usage Codex subscription usage
│   └── ui/              # TUI customization
├── skills/              # Agent Skills
├── memory-settings.json # Memory limits, summary model, and context settings
├── themes/pi.json       # Custom theme
├── keybindings.json     # Keybindings
└── settings.json        # Global Pi settings
```

Local files such as `auth.json`, `models.json`, `models-store.json`, `sessions/`, `memory.md`, `memory-index.md`, and `memories/` are not committed.

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
| `/context` | Show the context breakdown and preview the System Prompt, Tools, Context Files, or Skills |
| `/usage` | Show subscription usage, remaining allowance, and reset times for the current OpenAI Codex account |
| `/plan [task]` | Create a checklist, confirm it, execute it continuously, and run a final check |
| `/summarize` | Summarize the task and persist reusable indexed memory |
| `/memory_settings` | Interactively configure the memory limit, automatic summaries, model, thinking, notifications, and context sources |
| `/reload` | Reload extensions, skills, themes, and keybindings |
| `/login` | Configure provider authentication |
| `/model` | Select a model |
| `Ctrl+Y` | Open the session resume picker |

`memory_search`, `memory_get`, `memory_summarize`, `ask_question`, and `plan_check_result` are agent tools and do not need to be invoked manually.

Use `/memory_settings` to edit the configuration interactively, or edit `memory-settings.json` directly. It configures the memory limit, automatic summaries, summary model, thinking level, result display, and inclusion of Tool Messages or thinking. `summarize.includeToolMessages` controls both Tool Calls and Tool Results; Memory Tool calls and results are always excluded. Subsequent summaries use the saved configuration immediately; after toggling automatic summaries, run `/reload` to synchronize `memory_summarize` tool registration. `summarize.resultDisplay` supports `message` (write to the conversation), `popup` (centered popup, default), and `none` (no notification). Missing files or fields use built-in defaults. `memory_get` compares the latest memory version with its most recent read in the active branch: unchanged content reuses the previous Tool Result, while updated content returns full fresh details. Context compaction or a branch summary permits a full reload. In TUI mode, summaries run in the background without blocking the editor or subsequent turns. Press `Esc` to cancel the model request; cancellation is disabled once writes begin to preserve index/detail consistency. The popup emphasizes results, memory titles, key fields, input/output context usage, and elapsed time.

## Privacy and Security

Always inspect staged changes before publishing:

```bash
git status --short --ignored
git diff --cached
```

`.gitignore` only protects files that are not already tracked. If a sensitive file has ever been committed, remove it from the Git index and history, then rotate the affected credentials immediately.

`/context` details are shown only in the local TUI and are not written to the session or sent to the model again. `/usage` does not read credential files directly: it uses Pi's resolved runtime authorization and sends only Bearer Authorization to the official `https://chatgpt.com` usage endpoint; custom or proxy origins are rejected.

Ignored data includes:

- Provider credentials and model API keys
- Session history and exported JSONL files
- User profiles, memory indexes, and project memory details
- npm/git packages installed by Pi
- Local dependencies, caches, logs, and temporary files

## Notes

This configuration is tailored to a personal workflow and is not a general Pi distribution. Extensions have full local system access; review all extension source code before using or modifying third-party extensions.

`/usage` relies on an undocumented ChatGPT usage endpoint whose fields or availability may change. It currently supports only the official OpenAI Codex origin.
