# Third-party UI views

[中文](README.md)

This package owns the optional extensions' actual UI behavior, not only their wire types. It must never import `internal/ui` or own capability validation/persistence.

| File | Ownership |
| --- | --- |
| `dialog.go` | Small generic modal view and callbacks; third-party protocol dispatch |
| `questionnaire.go` | Form state, answer editing, review, cancellation, tabs and message/help presentation |
| `worker_settings.go` | Settings drafts and menu flow; submitted values still go to the backend |
| `codex_fast.go` | Per-backend state, async RPC refresh, sidebar geometry/rendering and notifications |
| `worker_session.go` | Transcript polling/replay, generation results, unavailable/partial-record display, conversation document construction |
| `worker_input.go`, `worker_report.go` | Contract/report formatting and collapsible list presentation |
| `conversation.go` | Recorded-conversation inspection target geometry |

The host retains only lifecycle bindings: selecting an originating tab/inspector, translating generic viewport coordinates, closing/focusing a modal, scheduling `tea.Cmd`, and rejecting replies whose owner is no longer displayed. It passes a small `DialogHost`/`Dialog` view, `CodexView` layout inputs, or a recorded-session pointer. No complete application `Model` is exposed or duplicated.

`internal/ui/extension_*.go` are thin forwarding bindings for existing host call sites. Generic UI tests remain integration coverage; direct tests here exercise the adapters without constructing the host model.

```bash
cd pi-goui
go test ./internal/ui ./internal/extensions/hostui/...
GORACE=atexit_sleep_ms=0 go test -race ./internal/ui ./internal/extensions/hostui/...
```
