# Generic full tool-detail host

[中文](README.md)

This package owns native Pi tool detail views and generic fallback, **not third-party extension layouts**. Third-party implementations live in [`../../extensions/toolrender/detail`](../../extensions/toolrender/detail/).

## Boundaries

- `render.go`: `Render(item, theme, width, hooks) []string`, error precedence, attachment/remaining-data fallback. Its default route asks the optional adapter and preserves generic rendering when it declines.
- `helpers.go`: generic heading/field/complete-value presentation. JSON decoding is shared via `render/tooldata`, preserving `json.Number` precision.
- `read.go`, `write.go`, `edit.go`, `bash.go`, `codemode.go`: native tool views. Source highlighting, diffs and complete literal output still use host hooks.
- `generic.go`: arbitrary saved parameters and results, including unknown fields.
- `extensions.go`: thin callback binding and the `WorkerTasks` compatibility facade. It contains no Worker/questionnaire/SoL-Pi/web rendering rules.

## Adding a view

For an extension, add its implementation under `internal/extensions/toolrender/detail`, and register it in that package's `Renderer.Render`. Use `extensionapi.DetailHost` for generic formatting rather than importing this host implementation. An unrecognized tool must return false and preserve fallback. Built-in Pi tools may add a local view here.

All hooks are per-render, pure and required. Inputs are display-only copies; preserve unknown fields, genuine blank lines, narrow-width safety and complete recorded output. Do not read files, reconstruct missing upstream history, impose card-preview limits on full detail, or mutate capability state.

```bash
cd pi-goui
go test ./internal/render/... ./internal/extensions/toolrender/...
```

Existing host integration tests cover real ANSI, width, dispatch and fallback. Adapter-local tests cover independent callback instances and isolated Worker projections.
