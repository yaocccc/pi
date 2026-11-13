# Optional Pi capabilities and UI adapters

[中文](README.md)

Install these extensions into your own Pi environment from any checkout directory. Neither replacing `~/.pi/agent` nor using the Go frontend is required. Extensions have local machine access: review them first. **Do not copy the repository's root settings, models, auth, or other personal configuration.**

## Installation

Requirements: Node.js **22.19+**, npm, and a Pi **1.5.x** host supporting the current extension API. The development lockfile does not upgrade your global host; older hosts may lack independent tool renderer registration.

From your checkout:

```bash
npm ci --ignore-scripts --prefix ./extensions
pi install /absolute/path/to/checkout/extensions
```

Replace the example path with your actual checkout. The second command registers a local package in Pi settings; it does not replace your agent directory. Keep the checkout available. Back up your settings first if desired. No npm publication or install hooks are needed. If the source already lives under Pi's auto-discovered agent/extensions directory, **do not register the same extensions again**.

For a temporary trial without registering the package:

```bash
pi -e /absolute/path/to/checkout/extensions/ask-question/index.ts \
   -e /absolute/path/to/checkout/extensions/pi-ui/index.ts
```

Configure your own models and credentials in Pi (for example, `/login` in native Pi). Never reuse the author's credentials or personal model identifiers.

The manifest enables all entries below and includes the Worker orchestration Skill. To select resources, merge a filtered package entry into your own Pi settings:

```json
{
  "packages": [{
    "source": "/absolute/path/to/checkout/extensions",
    "extensions": ["ask-question/index.ts", "pi-ui/index.ts"],
    "skills": []
  }]
}
```

Do not replace your entire settings file. `pi config` can also select resources. Alternatively, copy just a capability directory and its `pi-ui/<module>/` directory into `<target>/<module>/` and `<target>/ui/<module>/`. No `shared/` or full source tree is needed. Keep all files within both directories, not just `index.ts`. Each module README includes executable copy commands and optional manual native-UI registration.

## Copy-only module installation

For `ask-question`, `worker`, `codex-fast`, `sol-pi`, `autoname` or `filter-output`, copy `extensions/<module>/` and `extensions/pi-ui/<module>/` into `<target>/<module>/` and `<target>/ui/<module>/`. Load both `index.ts` entries, or load the capability and explicitly call the UI's default export from your own extension factory. The full-package `pi-ui/index.ts` is not required and must not be loaded again.

Pi 1.5.2 supplies SDK/TUI/TypeBox dependencies for these modules. No repository `node_modules`, `shared/`, generation step or author configuration is needed. Worker routes and other capability settings still require your own values. Telegram has no UI module and separately needs its external npm dependency. Use the individual guides below for complete commands.

Maintainers only: `npm run sync:portable --prefix extensions` updates checked-in thin helper copies; `node extensions/scripts/sync-portable.mjs --check` detects drift. Business logic stays in its capability; event types and display text stay in their owning module. Copy consumers never run these scripts.

## Features and prerequisites

| Entry | Responsibility and prerequisites |
| --- | --- |
| [ask-question](ask-question/README_EN.md) | Questionnaire queue, cancellation, answer validation; requires pi-ui or another interaction port, otherwise returns unavailable and unanswered questions |
| [worker](worker/README_EN.md) | Independent tasks; configure your own three model/Thinking presets; path checks are not a sandbox |
| [codex-fast](codex-fast/README_EN.md) | Codex request tiers; defaults to normal and only applies to supported providers |
| [pi-ui](pi-ui/README_EN.md) | Optional native UI and RPC compatibility bridge; owns settings commands, not execution |
| [sol-pi](sol-pi/README_EN.md) | Large-result archival and online compaction; see its model and economic settings |
| autoname | Automatic naming; configurable/disableable through autoname.json in the agent directory; may call a model |
| filter-output | Common sensitive-text filtering, not a comprehensive security or redaction guarantee |
| telegram | Optional messaging, enabled by PI_TG_TOKEN and PI_TG_CHAT |

Enabled by default does not mean configuration-free. Configure your own `worker-settings.json`, rather than copying the author's file. Configuration and archives belong to the **current user's Pi agent directory** (default `~/.pi/agent`, overridden by `PI_CODING_AGENT_DIR`). Source installation does not transfer models, credentials, or saved settings between users.

See [frontend boundaries](shared/FRONTENDS_EN.md). The Go frontend treats these capabilities as optional third-party extensions; it does not own their persistence or business rules. The package also works without Go.

## Third-party UI integration, one README at a time

Each guide below includes its own startup/discovery sequence, wire examples, lifecycle/error handling and absent-extension fallback. Shared design notes are optional background, not required to implement the minimal UI. In-process capability methods are explicitly distinguished from cross-process Pi RPC.

| Extension | English | 中文 | UI surface |
| --- | --- | --- | --- |
| ask-question | [Guide](ask-question/README_EN.md) | [指南](ask-question/README.md) | Standard dialogs or a complete structured questionnaire |
| worker | [Guide](worker/README_EN.md) | [指南](worker/README.md) | Tool details, settings dialogs, optional transcript RPC |
| codex-fast | [Guide](codex-fast/README_EN.md) | [指南](codex-fast/README.md) | State/get/set RPC or a settings dialog |
| sol-pi | [Guide](sol-pi/README_EN.md) | [指南](sol-pi/README.md) | Read-only plan/progress/archive presentation |
| autoname | [Guide](autoname/README_EN.md) | [指南](autoname/README.md) | Rename facts and standard session metadata |
| filter-output | [Guide](filter-output/README_EN.md) | [指南](filter-output/README.md) | Redaction facts and standard tool output |
| telegram | [Guide](telegram/README_EN.md) | [指南](telegram/README.md) | No dedicated UI API; delivery is backend-only |
| pi-ui | [Guide](pi-ui/README_EN.md) | [指南](pi-ui/README.md) | Optional dialog/fact/RPC transport adapter |

Use `PI_UI_PROTOCOL=1` to request structured interaction from any RPC frontend. Legacy Go flags and wire names remain compatible. It does not enable undocumented methods or bypass capability validation.

## Updating and removing

After updating your checkout, repeat `npm ci --ignore-scripts --prefix ./extensions`. Native Pi supports `/reload`; restart the Pi backend used by Go. Never load both a top-level entry and its internal entries.

`pi remove /absolute/path/to/checkout/extensions` removes package registration, not user settings, sessions, or archives. For temporary loading, stop supplying `-e`. Auto-discovered entries must be moved out of their discovery directory separately.

## Validation and limitations

```bash
npm --prefix extensions run test:package
npm --prefix extensions run typecheck
npm --prefix extensions test
```

The test script runs TypeScript tests from the repository root using temporary HOME / agent directories and cleared frontend markers, without real user credentials. It **excludes** SoL-Pi's .mjs suite. Testing does not require installing the checkout into your personal agent directory.

SoL-Pi's historical isolated runner requires Pi **1.0.4**. Its full suite has reproduced baseline failures on the current 1.5.x host; successful TypeScript tests do not imply a green full repository suite. See [SoL-Pi offline validation](sol-pi/README_EN.md#offline-validation). Manual terminal interaction, other platforms, and other host versions require separate validation.
