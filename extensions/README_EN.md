# Optional Pi Extensions

[中文](README.md) · [Repository](../README_EN.md)

Capabilities own execution, authoritative validation, state, and persistence. Optional `pi-ui` owns native presentation, interaction, and the RPC bridge. Capabilities load without a UI; unsupported interaction is unavailable, never consent. See [pi-ui](pi-ui/README_EN.md) for development boundaries.

## Install the whole package

Use your existing Pi installation and your own models/credentials. The current installation and TypeScript test baseline is **Node 22.19+ / Pi SDK 1.5.2**; this does not certify every historical suite against that version.

```sh
SRC=/path/to/repository/extensions
npm ci --prefix "$SRC"
pi install "$SRC"
```

[package.json](package.json) declares entries in `pi.extensions`. Installation registers the local package without copying the author's settings, upgrading global Pi, or installing other packages from the root settings. Authenticate with `/login` in native Pi first; configure all three Worker model presets yourself.

If Pi already discovers the code in the agent's `extensions/`, do not also `pi install` or load it explicitly. Pi discovers top-level .ts files and directories containing index.ts; internal entries, maintenance helpers, and tests are not additional extensions.

To select capabilities, list entries in your own Pi settings instead of registering the whole package:

```json
{
  "extensions": [
    "/path/to/repository/extensions/ask-question/index.ts",
    "/path/to/repository/extensions/worker/index.ts",
    "/path/to/repository/extensions/pi-ui/index.ts"
  ]
}
```

The whole-package UI degrades safely when other capabilities are absent. Test selected entries with `pi --no-extensions -e /path/to/capability/index.ts -e /path/to/ui/index.ts`.

## Copy one module

ask-question, worker, codex-fast, sol-pi, autoname, and filter-output support directory-pair distribution:

```text
extensions/<module>/         → <target>/<module>/
extensions/pi-ui/<module>/   → <target>/ui/<module>/
```

Copy both **complete directories**, preserve the layout, and load the capability/UI index.ts entries—or call `registerUI(pi)` from your own entry, without double registration. Helpers ship with each module; no shared directory, node_modules, build, or generation step is required. Pi supplies SDK dependencies. Each capability README is the pair's sole integration guide, with exact copy commands, registration, and RPC examples.

Telegram is not one of these six pairs and needs an external dependency; follow its guide. Third-party frontends may omit native presentation, but the current structured RPC interactions still require a bridge adapter. Removing all UI code does not automatically provide those protocols.

## Third-party UI: use the per-extension README

| Module | Purpose and entry | Guide |
| --- | --- | --- |
| ask-question | Agent tool `ask_question`; queueing, cancellation, answer validation | [Questionnaires](ask-question/README_EN.md) |
| worker | Agent tool `worker`; UI command `/worker_settings` | [Scheduling, settings, read-only recordings](worker/README_EN.md) |
| codex-fast | UI command `/codex-fast`; backend tier policy and persistence | [Fast / Ultrafast](codex-fast/README_EN.md) |
| sol-pi | `obs_recall`, `update_plan`; archives and online compaction | [Configuration, protocol, compatibility](sol-pi/README_EN.md) |
| autoname | Automatic primary-session names; may make extra model requests | [Naming policy](autoname/README_EN.md) |
| filter-output | Heuristic sensitive-data filtering of successful tool results | [Scope and limitations](filter-output/README_EN.md) |
| telegram | Environment-configured outbound notifications, no remote control | [Dependency and data disclosure](telegram/README_EN.md) |
| pi-ui | Optional host adapter for these capabilities; does not replace execution | [Boundaries, discovery, and RPC](pi-ui/README_EN.md) |

Each capability guide owns its usage, configuration, and wire examples; no reverse-engineering of Go is needed. Disable specialized controls without the capability/bridge, fall back to generic rendering for unknown payloads, and never invent remote methods or read extension-private files directly.

## Update, uninstall, and maintain

- After updating local source, rerun `npm ci --prefix "$SRC"`. Use `/reload` in native Pi or restart the Go/RPC backend; Go's `/reload` only rereads frontend preferences.
- Unregister the whole package with `pi remove "$SRC"`; for explicit/copy installations, remove the relevant settings and UI registration. Configuration, recordings, and archives require separate cleanup as documented by each module.
- [shared/](shared/) and [pi-ui/shared/](pi-ui/shared/) contain maintenance templates, type aggregation, and compatibility helpers, not standalone entries. After changing portable helpers, run `npm --prefix extensions run sync:portable` and inspect the diff; consumers do not run it.
- Maintain bilingual READMEs only for this overview, capabilities, and pi-ui—not duplicate guides in shared or UI subdirectories. Skills and role prompts are runtime resources, not disposable duplicate documentation.

Verify from the repository root:

```sh
npm ci --prefix extensions
npm --prefix extensions run typecheck
npm --prefix extensions test
```

Tests cover documentation JSON/real schemas, copy layouts, helper synchronization, capability boundaries, and RPC. The historical SoL-Pi .mjs suite is **not included** in npm test; see its [version limits](sol-pi/README_EN.md#compatibility-and-validation). These checks do not replace real-terminal, cross-platform, or model end-to-end verification.
