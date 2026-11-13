# Pi UI 适配器与能力边界

[English](README_EN.md) · [安装与模块索引](../README.md)

可选适配层：原生工具绘制、问卷、设置菜单、通知、刷新及结构化 RPC 桥。整包加载 index.ts；独立复制按对应能力 README 使用 `pi-ui/<模块>/`，不依赖整包入口。不要仅复制整个 pi-ui 目录后假定它独立；整包入口仍引用兄弟能力/shared。

## 归属与进程内接口

- 能力拥有执行、权威校验、配置保存、取消、请求生命周期、历史与结构化状态；不得导入 pi-ui/pi-tui、调用 ctx.ui 或注册渲染器。
- UI 只消费接口，使用 `pi.registerToolRenderer` 附加展示，不覆盖工具或 execute。Go/Web 前端自行绘制，不能直接复用原生组件。
- 各模块 capabilities.ts 的 `registerCapability/getCapability` 使用宿主事件总线 `pi:capability:discover:v1`，无模块全局 registry；按需发现，不依赖加载顺序。它是进程内方法接口，**不是远程 RPC**。
- 各能力 frontend.ts 发布 `pi:frontend-event:v1` 的 `{context,event}` 事实；发布不判断前端身份，事实数据发布为独立、深度只读的快照，归属信封不可替换；宿主 context 保留原引用而不冻结。观察者故障不得改变业务结果。UI 决定通知/文案，设置成功事实必须在后端保存之后发布。
- shared 与 pi-ui/shared 是发现、传输、会话策略、token helper 的维护模板/兼容聚合；副本随模块分发。主会话/Worker 策略显式判断，不以 hasUI 决定业务执行。

| 能力 | 接口与职责 |
| --- | --- |
| `ask-question:v1` | [api.ts](../ask-question/api.ts)：ask/pending，能力拥有 FIFO、取消和校验 |
| `ask-question.interaction:v1` | 同上：UI 实现 supports/ask，接收问题和取消信号，返回未可信答案 |
| `worker:v1` | [api.ts](../worker/api.ts)：会话快照/历史/订阅、设置/模型/保存、限量读取记录 |
| `codex-fast` | [api.ts](../codex-fast/api.ts)：支持档位/读取/保存；codex-fast:changed 为成功保存事实 |

无交互适配器时问卷返回 unavailable 与未回答问题，不伪造取消或同意。问卷 UI 管编辑器占用/关闭；Worker 只给 detached snapshots，UI 持有刷新计时器。关闭或替换会话须取消订阅、清理计时器与待处理请求，不能把旧所有者结果交给新页面。

`/worker_settings`、`/codex-fast` 是 UI 命令，不是能力入口。第三方宿主可直接消费进程内服务，不必模拟菜单。后端权威校验与私有存储不能移到前端。Worker 的设置结构校验、错误类型和记录 DTO 归 api.ts；UI 可做结构预检，但模型有效性、思考档位和落盘仍由 saveSettings 最终复核，不导入 config.ts/settings.ts 的存储实现。

## 第三方 UI：发现与传输

加载对应适配器后，按 ctx.mode=rpc 默认启用结构化 UI，PI_UI_PROTOCOL / PI_GOUI_PROTOCOL 不再是开关，无需设置。仅问卷仍保留非 TUI 下 PI_GOUI_ASK_QUESTION=1 的旧兼容路径。历史 `pi-goui:*:v1` wire 名称不要求客户端是 Go。

1. 在对应 Pi 后端调用 get_commands，仅使用已发现且 source=extension 的 pi-ui-rpc、worker_settings、codex-fast。工具/能力不在命令表中，不能由此推断它们不存在。
2. 未知斜杠命令可能落入模型输入，**不能为探测而发送**。无桥时禁用专项入口，保留 native Pi/codemode/通用工具显示。
3. 每个后端、会话和请求独立关联 ID。忽略迟到响应；页面关闭、后端替换、取消或超时须清理等待者。桥不替客户端设置超时。

### 通用桥

只有以下方法；参数与业务校验由能力处理，桥仅路由并验证传输：

| method | params |
| --- | --- |
| codex-fast.get | {} |
| codex-fast.set | {level:"normal"\|"fast"\|"ultrafast"} |
| worker.transcript | {descriptor:{version:1,id:UUID},cursor?} |

没有 worker.start/cancel/settings.get/settings.save、plan.get/set、sol-pi.compact 或 observation.read 远程方法。Pi abort 不是指定 Worker 批次的控制 API。

完整只读请求（UUID 仅作示例，实际必须来自任务 descriptor）：
```json
{"id":"rpc-1","type":"prompt","message":"/pi-ui-rpc {\"id\":\"read-1\",\"method\":\"worker.transcript\",\"params\":{\"descriptor\":{\"version\":1,\"id\":\"11111111-1111-4111-8111-111111111111\"}}}"}
```

普通 response 只确认外层 rpc-1 命令。实际结果是另一条通知：
```json
{"type":"extension_ui_request","id":"notice-1","method":"notify","notifyType":"info","message":"pi-ui:rpc:v1:{\"id\":\"read-1\",\"result\":{\"cursor\":\"22222222-2222-4222-8222-222222222222\",\"reset\":true,\"records\":[],\"more\":false,\"partial\":false}}"}
```

只剥离精确 `pi-ui:rpc:v1:` 前缀，按内层 read-1 关联，忽略通知 id；不发送 extension_ui_response。成功为 {id,result}，失败为 {id,error:{code,message}}，如：
```json
{"id":"read-1","error":{"code":"unavailable","message":"Extension capability unavailable"}}
```

传输错误包括 invalid-request/unavailable/extension-error/response-too-large；业务错误见能力指南。notify.message **再次 JSON 编码后的 UTF-8 大小**上限为 32 MiB，超限返回短错误，不应断开后端。未知消息、畸形载荷和未匹配 ID 不更新状态。

记录只通过 worker.transcript 获取：后端验证 descriptor/路径/游标并限制读取。前端不猜路径、不制造游标、不读第三方私有文件；分页与缺失记录处理见 [Worker](../worker/README.md)。

### 对话框与事实通知

| wire | 处理 |
| --- | --- |
| input/title=pi-goui:ask-question:v1 | placeholder JSON 问卷；回传 value JSON 字符串或 cancelled:true，见 [问卷](../ask-question/README.md) |
| input/title=pi-goui:worker-settings:v1 | placeholder 含 config/original/models/error；见 [Worker 设置](../worker/README.md#设置事务) |
| input/title=pi-goui:codex-fast:v1 | 选择档位字符串，见 [Codex Fast](../codex-fast/README.md) |
| notify/message=pi-goui:event:v1:… | 解析 {name,data} 事实，不回复；name/data 由各能力指南定义 |

问卷由后端串行/取消/校验，前端保留请求 id、原后端/标签归属和取消语义；不能按当前焦点回包。不可用交互不等于授权，关闭显示不能绕过后端生命周期。通知未知字段可忽略，畸形/未知事件降级；历史事实不重放成新提示，缺通知不证明保存失败。

## 扩展与验证

Go 实现在 [pi-goui/internal/extensions](../../pi-goui/internal/extensions/README.md)，通过 core 定义的接口按实例装配；不读写 Codex 配置或 Worker 私有文件。其他前端应保留相同后端归属和安全降级边界。

新增能力先定义能力自己的接口/事实，再增加 UI 适配；执行、授权和持久化仍留后端。通用开发与测试命令见[扩展总览](../README.md#更新卸载与维护)；边界调整需同时检查原生 TUI、Go RPC、普通 RPC 与无 UI 路径，不能仅以一套渲染快照证明兼容。
