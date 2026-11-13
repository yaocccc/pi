# Third-party tool views

[中文](README.md)

All extension-specific compact cards and full-detail rendering belongs here. These packages do not execute tools, read extension files, or modify settings.

| Package | Responsibility |
| --- | --- |
| `cards` | Worker task cards, activities, questionnaire cards, Codex labels |
| `detail` | Worker, questionnaire, SoL-Pi and web full-result views; stable task projections |
| `internal/extensionapi/render.go` | Generic callback/type contract |

## Host integration

- `cards.New(extensionapi.CardHost)` binds sanitization, wrapping, painting, framing and expansion helpers to one renderer. `WorkerLayout` returns rows plus generic hit blocks; `QuestionRows` returns compact rows.
- `detail.New(extensionapi.DetailHost).Render(name,item,args,details)` returns false for unknown tools; the host then renders its normal fallback. Rows append to the host-owned per-call slice. Generic complete-value formatting remains in the host, not duplicated here.
- `detail.WorkerTasks(item)` returns isolated display snapshots and stable task keys, preserving unknown task fields without exposing sibling task content. This is not a capability or task-control API.

Dependencies may include generic `session`, `config`, `render/frame` and `render/tooldata`, but never the parent `render` package, `render/tooldetail`, or `ui`. Each host binding is instance-local; there is no mutable global rendering registry. Core compatibility facades remain thin.

```bash
cd pi-goui
go test ./internal/render/... ./internal/extensions/toolrender/...
```

Host integration tests retain complete output/ANSI/width/folding/animation checks. Local tests enforce dependency direction, independent host bindings, stable slot codes and privacy-preserving task projection.
