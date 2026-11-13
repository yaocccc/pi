# 第三方 UI 视图

[English](README_EN.md)

本包承载可选扩展的实际 UI 行为，而不只是协议类型。禁止导入宿主 `internal/ui`，不接管能力端的权威校验或持久化。

| 文件 | 职责 |
| --- | --- |
| `dialog.go` | 小型通用模态视图与回调、第三方协议路由 |
| `questionnaire.go` | 问卷状态、答案编辑、确认、取消、标签以及消息/帮助文案 |
| `worker_settings.go` | 设置草稿与菜单流程；提交值仍交给后端处理 |
| `codex_fast.go` | 每后端状态、异步 RPC 刷新、侧栏几何/渲染与通知 |
| `worker_session.go` | 记录轮询/回放、代际结果、不可用/部分记录展示以及会话文档构建 |
| `worker_input.go`、`worker_report.go` | 契约/报告格式化与可折叠列表展示 |
| `conversation.go` | 已记录会话的查看入口几何 |

宿主仅保留生命周期绑定：选择来源标签/查看器、转换通用视口坐标、关闭/聚焦对话框、调度 `tea.Cmd`，以及拒绝其归属对象已不再展示的响应。边界只传递 `DialogHost` / `Dialog` 小型视图、`CodexView` 布局输入或已记录会话指针，不暴露或复制整个应用 `Model`。

`internal/ui/extension_*.go` 仅为现有宿主调用点保留薄转发。原 UI 测试继续提供集成覆盖；本包的直接测试无需构造宿主 Model 即可验证适配器。

```bash
cd pi-goui
go test ./internal/ui ./internal/extensions/hostui/...
GORACE=atexit_sleep_ms=0 go test -race ./internal/ui ./internal/extensions/hostui/...
```
