# 可选 Codex Fast 适配

[English](README_EN.md)

此包是第三方扩展适配，不是 Pi 核心功能。只包含 RPC 方法名和状态类型，不读取 agent 路径、配置文件，不迁移格式、选择请求档位或持久化。

- `Get(ctx, caller)` 调用 `codex-fast.get`，接收 `{level, levels}`。
- `Set(ctx, caller, level)` 调用 `codex-fast.set`，由后端校验并保存。
- `rpc.Client.CallExtension` 先通过标准 `get_commands` 发现可选的 `pi-ui-rpc` 扩展命令；没有桥或能力时，侧栏入口隐藏。
- `internal/ui/extension_codex_fast.go` 只持有当前后端的展示状态、控件交互和刷新，后端确认前不乐观更新。

传输格式为 `/pi-ui-rpc {"id":"...","method":"...","params":...}`；响应通过前缀 `pi-ui:rpc:v1:` 的 notify 传送 `{id,result}` 或 `{id,error:{code,message}}`。能力缺失返回 `unavailable`；取消只停止客户端等待，不回滚已保存操作。
