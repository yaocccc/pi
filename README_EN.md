# Pi Coding Agent Configuration

[中文](README.md)

Personal [Pi Coding Agent](https://pi.dev) configuration, optional extensions, and a standalone Go terminal frontend—not a general-purpose distribution. Reusable components do not require replacing your `~/.pi/agent` or copying the author's models, credentials, or root configuration.

## Demo

https://github.com/user-attachments/assets/37766643-9320-4af4-a068-c3702b6a8467

[Original MP4](pi-coding-agent-demo.mp4)

## Getting started

- **Go frontend only**: with Pi and Go 1.26+ installed, run the commands below; authenticate with `/login` in native Pi first. The frontend uses your Pi environment and works without this repository's extensions.
- **Optional capabilities**: follow the [extension guide](extensions/README_EN.md) for the whole package, or a module README to copy its capability/UI directory pair. Do not register code that Pi already discovers automatically.
- **Keep your own settings**: do not copy this repository's settings, models, or authentication configuration. Extensions have local machine privileges; review their source before running them.

```sh
git clone <repository-url> /path/to/pi-config
/path/to/pi-config/pi-goui/run.sh --cwd /path/to/project
```

See the [pi-goui guide](pi-goui/README_EN.md) for builds, platform dependencies, shortcuts, configuration, update notices, and verification. You can copy `pi-goui/` on its own or run the built binary independently. Native Pi's `/reload` reloads extensions; the Go command only rereads frontend preferences, so extension changes require restarting the backend.

## Documentation

| Need | Guide |
| --- | --- |
| Install, select, or maintain extensions | [Extension overview and module index](extensions/README_EN.md) |
| Use questionnaires, Worker, Codex Fast, or SoL-Pi | Each module README linked from the overview owns its configuration, protocol, and safety limits |
| Build a third-party UI | [Capability boundaries and RPC bridge](extensions/pi-ui/README_EN.md), plus module protocols |
| Modify the Go frontend | [Development rules](pi-goui/AGENTS_EN.md), [extension interfaces](pi-goui/internal/extensions/README_EN.md) |
| Understand Agent working rules | [Engineering rules](AGENTS.md), [Worker orchestration skill](extensions/worker/SKILL.md), [design-question skill](skills/grill-with-docs/SKILL.md) |

Root `settings.json`, `keybindings.json`, and module configurations are personal examples; consult the relevant guide and source for defaults and supported options. Tool names are not slash commands; never send an undiscovered command as a model prompt.

## Data and safety

- Keep credentials, model keys, sessions, and memories in ignored local files. Prefer environment-variable references for custom model secrets; see [Pi model configuration](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/models.md).
- `.gitignore` does not protect tracked files. Before publishing, inspect `git status --short --ignored` and `git diff --cached`. Remove exposed content and rotate leaked secrets.
- Worker path checks are not a shell/filesystem sandbox. The parent Agent must inspect actual diffs, existing changes, and verification evidence rather than trust summaries alone.
- Telegram can send task content to a chat; autonaming and online compaction send context to models and may incur extra charges. Confirm recipients before enabling them.
- Output filtering is heuristic. Worker recordings and SoL-Pi archives may contain raw sensitive data; they are not necessarily redacted or encrypted. Their module guides own retention and cleanup rules.
