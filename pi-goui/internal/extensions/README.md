# 可选第三方扩展 UI 适配

[English](README_EN.md)

Pi Go UI 是可独立运行的 Pi RPC 前端。本仓库的扩展与其他用户安装的扩展一样，**不是 Go 前端的核心依赖**。

## 代码归属

| 位置 | 职责 |
| --- | --- |
| `internal/rpc/` | 标准 Pi 传输，以及按能力发现启用的通用扩展 RPC 信封；不认识 Worker/Codex 操作 |
| `internal/extensions/` | 第三方协议识别、通知文案；未知请求保留标准 Pi 行为 |
| `askquestion/` | 问卷传输类型；不做权威答案校验 |
| `codexfast/` | `codex-fast.get/set` RPC 客户端；不读写配置或迁移旧格式 |
| `worker/` | 设置传输类型、记录 RPC 与只读回放；不读取扩展私有日志 |
| `solpi/` | 只读计划投影；不压缩上下文或修改计划 |
| `toolrender/cards/` | Worker/问卷紧凑卡片、任务活动与 Codex 档位标签 |
| `toolrender/detail/` | Worker/问卷/SoL-Pi/Web 工具完整详情，保留未知字段 |
| `internal/extensionapi/render.go` | 中立绘制类型和宿主回调，不含第三方行为 |
| `internal/render/extensions.go`、`internal/render/tooldetail/extensions.go` | 仅绑定宿主回调、类型别名与薄路由 |
| `hostui/` | 通过中立宿主回调实现第三方控件、表单和查看器 |
| `internal/session/extension_plan.go` | 将可选计划数据接入通用侧栏状态 |

工具渲染实现在 `toolrender/`，不再仅以宿主包中的文件名前缀区分。适配器接收通用换行、净化、边框和完整字段展示回调，不导入宿主 `render` 或 `ui` 实现。共享 JSON 解码位于 `render/tooldata`，不包含扩展规则；未知工具仍使用宿主通用结果视图。宿主集成测试保留在 `render`，适配器本地测试覆盖回调隔离与协议投影。

## 可选 RPC 桥

`extensions/pi-ui` 在 Pi 后端提供 `/pi-ui-rpc`。Go 先调用 `get_commands`，确认该名称属于 **extension** 命令。缺少桥时返回 `ErrExtensionUnavailable`，不发送可能触发模型的未知斜杠提示词。

各适配器注册方法并委托能力服务；目前使用 `codex-fast.get`、`codex-fast.set` 和 `worker.transcript`。响应按请求 ID 关联；错误、取消和进程结束会清理本地等待者。客户端取消不会撤销后端已提交的操作。

没有桥或相应能力时，不显示 Codex 档位控件；Worker 查看器回退到已有任务结果，不尝试读取本地扩展文件。Go 与 TypeScript 只通过 Pi RPC 进程通信，不直接导入扩展、不通过共享文件交互，也不假定仓库位置。

## 仍属于前端的能力

会话浏览、剪贴板/编辑器、图片预览、Git 展示、额度展示、更新通知和前端偏好等通用 UI 辅助能力仍在前端，不依赖这些第三方扩展。这不等于声称整个应用完全没有本地 I/O。

新增适配时，将协议/客户端/只读投影放在此处，权威校验和持久化留在后端，并覆盖扩展缺失及通用回退测试。Go 不会直接加载第三方 TypeScript 包。
