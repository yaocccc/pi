# 双前端边界

Go UI (`pi-goui/`) 是主界面，原生 Pi TUI 仍受支持。扩展共享执行、校验、持久化、取消、历史和结构化状态；两端各自决定布局、交互和通知文案。不要用 `ctx.hasUI` 判断原生模式：RPC 也可能有 UI。

## 路由与协议

- 原生组件仅在 `ctx.mode === "tui"` 执行；`renderCall` / `renderResult` 是原生适配层，继续保留。
- Go 启动 RPC 子进程时设置 `PI_GOUI_PROTOCOL=1`。扩展通过 `isGoUI(ctx)` 同时检查 RPC 模式与该能力标记。
- 复用 SDK 的 `extension_ui_request`，不屏蔽通用 RPC UI 请求。普通 RPC 问卷、设置菜单保留兼容回退。
- Go 专用 input 的标题是协议标识，placeholder 为 JSON，响应 value 按下述格式返回；取消使用 SDK 取消响应，不保存草稿：
  - `pi-goui:ask-question:v1`: `{questions, review}` → `{answers, cancelled}`；仍兼容旧 `PI_GOUI_ASK_QUESTION=1`。
  - `pi-goui:worker-settings:v1`: `{config, models:[{id, thinking}], error?}` → 完整 RoutingConfig。扩展严格校验后原子保存，失败可重试，Go 负责草稿与放弃确认。
  - `pi-goui:codex-fast:v1`: `{level}` → 档位字符串 (`normal` / `fast` / `ultrafast`)，扩展负责保存及请求参数。
- 业务通知通过 info notify 传输 `pi-goui:event:v1:{"name":...,"data":...}`。这只是 RPC 载体，不是展示文本。Go 在 `internal/ui/extension_events.go` 生成通知；未知或损坏事件被消费，不泄漏 JSON。
- 命名、脱敏、上下文压缩事件的数据类型见 `frontend-events.ts`；原生文案集中于 `native-events.ts`。Worker/Codex 设置事件由各设置模块发出。普通非 Go RPC 不接收 Go 专用通知。

## 文件职责

- `ask-question/{model,schema}.ts`：数据；`index.ts`：工具与 FIFO；`native-ui.ts`：原生问卷/渲染；`rpc.ts`：协议及回答校验。
- `worker/{data,history,callbacks}.ts`：与 UI 无关的结果、快照、回调；`native-ui.ts`、`ui.ts`、`presentation.ts`：原生渲染和刷新；`settings.ts`：设置路由、校验、持久化；`settings-ui.ts`：菜单适配（原生及通用 RPC 兼容）。
- `token-estimate.ts`：纯 token 估算；`tool-tokens.ts` 与 `frame.ts`：原生格式和边框。
- Go 独立消费现有 `worker-ui` details。不要移除其中的状态数据，也不要将 ANSI 或原生组件传到 Go。
- Telegram/Herdr 是独立外部集成，不属于本地 UI。会话名称本身属于共享元数据，不是通知。

调整协议时同时检查 Go RPC 与原生 TUI；不要让 Go 的交互流程退回扩展中的原生菜单。
