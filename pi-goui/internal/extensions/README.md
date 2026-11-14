# 可选扩展接入指南

[English](README_EN.md)

此目录只适配第三方能力的协议、状态和展示，不执行后端业务。使用说明见[项目 README](../../README.md)，通用修改约束见 [AGENTS](../../AGENTS.md)。

## core 定义契约，extensions 实现并导出

权威接口在 [core/extension/contracts.go](../core/extension/contracts.go)，低层投影/绘制接口在 [core/extensionapi/](../core/extensionapi/)。入口为：

```go
type Extension interface {
    Capabilities() Capabilities
}
```

模块只声明实际支持的能力，nil 表示缺省。先确认后端协议，再实现所需接口，并加入 [extensions.New()](extensions.go) 返回的 `[]extension.Extension`；已有实现见 [hostui.Adapter](hostui/adapter.go)、[projectionAdapter](projection.go) 和 [toolrender.Adapter](toolrender/adapter.go)。支持现有能力无需在 core 增加模块专用分支；新增通用能力先扩充 core 契约。

| 能力入口 | 职责 |
| --- | --- |
| `NewSession` | 同步、异步更新、侧栏、滚动、轮询、事件及 `ControlView` 控件 |
| `OpenDialog` | `DialogHandler` 管理输入、确认、取消；经 `DialogHost` 回调宿主 |
| `NewInspector` / `NewInspectorContent` | `Inspector` 管理详情、交互、子页与内容版本 |
| `NewUsage` | 每轮新建独占观察器，消费权威记录并返回不重复计数的输入/输出用量；不得依赖活跃查看器、UI 回调或共享可变容器 |
| `NewProjection` / `Tools` | 会话投影与可选卡片/完整详情 Provider |

[core/ui/extensions.go](../core/ui/extensions.go) 是核心唯一实现导入点，只以 `extension.New(extensions.New())` 组装 Runtime。其他核心使用方只消费中立接口，不暴露整个 Model、具体类型别名或可变全局 registry。

该组合入口在启动时通过 [ConfigureSharedWorkingUsage](../runtime/working_snapshot.go) 为稳定执行者注入 `NewUsage` 工厂，helper 与普通 TUI 使用相同装配；不要在 runtime 增加第三方协议分支。共享工作行只传递汇总计数、时钟与 revision，不传输正文或回放完整工具结果；晚加入和原实例消费同一权威投影，独立模式仍使用同一累计算法。

WebUI 在网页终端中直接运行同一 TUI，复用上述扩展实现；无需浏览器专项适配或第二份扩展状态。

跨进程 RPC 与会话所有权由 [runtime](../runtime/) 管理，不移入扩展装配层。独立 TUI 共享后端业务状态，但各自保留本地交互投影；问卷只有成功写入回答后才广播 resolved，失败不得提前清除。持久扩展状态按原生 `setStatus` / `setWidget` / `setTitle` 协议补给晚加入者，`set_editor_text` 不作持久广播回放。

## 依赖与实现归属

- [hostui/](hostui/) 管理第三方表单、控件、查看器及私有交互状态，不导入 `core/ui`。
- [toolrender/cards/](toolrender/cards/) 与 [toolrender/detail/](toolrender/detail/) 管理专项卡片/完整详情；叶子包仅经中立回调复用绘制，不导入父 renderer/UI。只有 [toolrender/views.go](toolrender/views.go) 装配通用 render Provider；未识别工具交回通用兜底。
- [sessionview/](sessionview/)、[solpi/](solpi/) 和 [web/](web/) 管理实时/历史投影、指标及隐藏策略；[internal/protocol/](internal/protocol/) 管理私有 wire 识别。子包不得反向导入根装配包。
- [askquestion/](askquestion/)、[codexfast/](codexfast/) 和 [worker/](worker/) 保存必要传输类型、客户端与只读回放。权威校验、权限、调度、持久化、迁移及私有存储留在后端，不通过共享文件连接 Go 与 TypeScript。

## 组合与生命周期

- 按 slice 顺序选择首个工具、弹窗、查看器、控件或事件处理者；观察、会话同步与 polling 广播。usage 求和，各提供者不得重复贡献同一记录。
- 复合投影的计划取首个非空，隐藏策略取或，指标取首个可用值。历史成功验证后才提交快照；快照拒绝跨实例使用。Worker 回放在初始化/重置时建立独立投影。
- 每个 Model 一个 Runtime，每个标签、弹窗和工作轮次经工厂创建独立可变状态；ToolViews 是实例级不可变组合，嵌套查看器与降级视图继承同一 Provider。
- 私有状态改变详情行时，`Inspector.Version()` 必须变化；缓存区分 Provider 身份与版本。异步结果保留原后端、页面及 revision 归属。
- `Sessions.Control(ctx)` 返回绑定拥有者的控件；点击后的请求只发给该拥有者，不因布局/可见性变化重新匹配。`Sessions.Poll` 才广播。
- 空 slice、缺能力、未知载荷或格式错误不能破坏普通 Pi、codemode 与通用回退。看似协议的用户文本不能被当作命令。

## RPC 与降级

通用 [Client.CallExtension](../runtime/extensions.go) 先经 `get_commands` 确认 `name:"pi-ui-rpc", source:"extension"`，再发送 `/pi-ui-rpc {"id":…,"method":…,"params":…}`。响应是前缀 `pi-ui:rpc:v1:` 的 notify，载荷为 `{id,result}` 或 `{id,error:{code,message}}`，不是外层 prompt 确认。

缺桥返回 `ErrExtensionUnavailable`，不能发送可能触发模型的未知斜杠提示词。按请求 ID 关联回复；错误、取消、超时和进程退出清理等待者并忽略迟到响应。本地取消只停止等待，不回滚后端已提交的操作。

| 适配 | 必须保持的边界 |
| --- | --- |
| 问卷 | 标准 `extension_ui_request/response` 按对话 ID 归属；取消为 `cancelled:true`，不伪造答案或默认同意。答案权威校验由后端完成。 |
| Codex 档位 | 仅 `codex-fast.get/set`；采用后端返回的 `level/levels`，后端确认后再更新。缺桥/能力隐藏控件，不读配置、不从模型名推断档位。 |
| Worker | 仅 `worker.transcript`，参数为 `descriptor:{version,id}` 和可选不透明 `cursor`；消费记录及 `reset/more/partial`。路径、文件身份、偏移、限制和游标有效期均由后端验证。 |
| Worker 展示 | 使用稳定 `phaseCode`，不解析翻译文案；回放只重建展示，不执行任务。不将 Activity 当作完整历史，缺失 end 不代表成功；缺记录回退已有结果，部分记录保留并标记不完整。完整历史和批量事件在后台独占 Replay/投影副本上准备，按原查看器、descriptor 与任务代际提交；坏 header/reset 保留旧正文，后续 completed 不能掩盖错误。 |

没有公开的 `worker.start`、`worker.cancel` 或远程设置方法；`worker` 是 Agent 工具，`/worker_settings` 是对话命令。不要虚构 RPC 或读取本地私有日志兜底。

历史 `pi-goui:*:v1` 标题及 `pi-goui:event:v1:` 前缀是兼容 wire 标识，不要求客户端是 Go。后端 RPC 模式默认使用结构化 UI，无需协议环境变量，不应重命名这些标识。

新增适配必须覆盖正常路径、实例隔离、桥/能力缺失、未知协议、取消和迟到响应；专项测试放在实现旁，核心保留真正的宿主集成。验证命令统一见 [AGENTS](../../AGENTS.md#验证)。
