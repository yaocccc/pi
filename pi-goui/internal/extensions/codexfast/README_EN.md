# Optional Codex Fast adapter

[中文](README.md)

This package is a third-party extension adapter, not a Pi core feature. It contains only RPC method names and the extension's state DTO. No agent path, configuration file, migration, model-tier policy, or persistence is known to Go.

- `Get(ctx, caller)` calls `codex-fast.get` and receives `{level, levels}`.
- `Set(ctx, caller, level)` calls `codex-fast.set`; the backend validates and commits.
- `rpc.Client.CallExtension` discovers the optional `pi-ui-rpc` extension command via standard Pi RPC before invoking it. Without the bridge/capability the sidebar contribution stays hidden.
- `internal/ui/extension_codex_fast.go` owns only the optional control's interaction, per-backend display state, and refresh. It never optimistically treats a selection as committed.

The transport envelope is `/pi-ui-rpc {"id":"...","method":"...","params":...}`. Responses are correlated `extension_ui_request` notifications with `pi-ui:rpc:v1:` and `{id,result}` or `{id,error:{code,message}}`. Capability absence is `unavailable`; transport cancellation only stops waiting and does not roll back an already committed operation.
