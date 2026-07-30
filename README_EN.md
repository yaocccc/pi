# Pi Coding Agent Configuration

[中文](README.md)

This is my personal [Pi Coding Agent](https://pi.dev) configuration, including custom extensions, themes, keybindings, and workflows. The repository only stores public and reusable configuration source code. Credentials, model secrets, sessions, and personal memories are excluded by `.gitignore`.

## Features

- **Custom UI**: startup logo, editor, message cards, working status, and a compact footer.
- **Structured questions**: the `ask_question` tool supports single choice, multiple choice, and custom input.
- **Plan workflow**: `/plan` creates a plan, waits for confirmation, executes and reviews each todo, then performs a final review.
- **Tiered memory**: automatically maintains user preferences, retrieves indexed memories through `searchmemory`, and persists reusable project knowledge with `/end`.
- **Sensitive output filtering**: redacts common API keys, tokens, private keys, and connection strings before tool results enter the model context.
- **Request optimization**: enables the priority service tier for supported OpenAI Codex GPT-5.6 models.
- **Packages**: integrates `pi-web-access` and `@ff-labs/pi-fff`.
- **Theme and keybinding**: includes the custom dark `pi` theme and binds `Ctrl+Y` to the session resume picker.

## Structure

```text
.
├── extensions/          # TypeScript extensions
│   ├── memory/          # Tiered memory, memory tools, and /end
│   ├── ask-question.ts  # Structured user questions
│   ├── fast.ts          # Model request optimization
│   ├── filter-output.ts # Sensitive output filtering
│   ├── plan.ts          # /plan workflow
│   └── ui.ts            # TUI customization
├── skills/              # Agent Skills
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
| `/plan [task]` | Plan, confirm, execute, and review a task |
| `/end` | Finish the task and persist reusable indexed memory |
| `/reload` | Reload extensions, skills, themes, and keybindings |
| `/login` | Configure provider authentication |
| `/model` | Select a model |
| `Ctrl+Y` | Open the session resume picker |

`searchmemory`, `ask_question`, `plan_set_todos`, and `plan_check_result` are agent tools and do not need to be invoked manually.

## Privacy and Security

Always inspect staged changes before publishing:

```bash
git status --short --ignored
git diff --cached
```

`.gitignore` only protects files that are not already tracked. If a sensitive file has ever been committed, remove it from the Git index and history, then rotate the affected credentials immediately.

Ignored data includes:

- Provider credentials and model API keys
- Session history and exported JSONL files
- User profiles, memory indexes, and project memory details
- npm/git packages installed by Pi
- Local dependencies, caches, logs, and temporary files

## Notes

This configuration is tailored to a personal workflow and is not a general Pi distribution. Extensions have full local system access; review all extension source code before using or modifying third-party extensions.
