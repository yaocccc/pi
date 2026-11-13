# 能力与前端边界

[English](FRONTENDS_EN.md) · [安装](../README.md)

**双目录分发：**能力目录与对应 `pi-ui/<模块>/` 组成自包含复制单元，目标为 `<目标>/<模块>/` 和 `<目标>/ui/<模块>/`。使用者无需本 `shared/` 目录或整包 UI 入口。发现/会话策略/传输 helper 在这里保留维护模板，模块内已有源码副本；事实类型及专项展示各归所属模块。`npm --prefix extensions run sync:portable` 仅供维护者同步，不是安装前提。

## 依赖方向

- `extensions/worker`、`ask-question`、`codex-fast`、`sol-pi` 等只提供能力：执行、权威校验、配置持久化、取消、请求生命周期、历史和结构化状态。
- `extensions/pi-ui` 是可选 Pi 宿主适配层，统一负责原生绘制、菜单、草稿、确认、通知文案和刷新。能力模块不得导入它、`pi-tui`，也不得调用 `ctx.ui` 或注册渲染器。
- `pi-ui → 能力接口`，不能反向依赖。`pi.registerToolRenderer` 独立附加展示，不覆盖能力工具及执行方法。
- `pi-goui` 自行绘制 Go 界面；未来 Web UI 可实现自己的适配层。结构化工具结果和 RPC 协议不是原生 UI 实现。

## 服务与交互端口

各模块内 `capabilities.ts`（维护模板位于 `shared/capabilities.ts`）的 `registerCapability` / `getCapability` 使用宿主会话事件总线按名称发现接口，没有模块全局注册表。消费者在使用时查找服务，因此无须规定扩展加载顺序。它是**进程内方法接口**，并非可直接序列化的远程协议；远程前端需要桥接器。

| 名称 | 定义 | 能力 |
| --- | --- | --- |
| `worker:v1` | `worker/api.ts` | 会话快照、历史、变化订阅；设置读取、可用模型、校验保存；限量读取任务记录 |
| `ask-question:v1` | `ask-question/api.ts` | `ask`、待处理数量；统一串行、取消、结果校验 |
| `ask-question.interaction:v1` | `ask-question/api.ts` | 前端实现 `supports` / `ask`；接收结构化问题和取消信号，提交未可信结果 |
| `codex-fast` | `codex-fast/api.ts` | 支持档位、读取配置、校验保存；`codex-fast:changed` 发布成功保存事实 |

没有适配器时能力仍能加载运行；问卷返回 `unavailable` 及未回答的问题，不假装用户取消或已同意。问卷服务拥有 FIFO、pending 和会话关闭生命周期，适配层负责编辑器占用与关闭显示。Worker 只发布 detached snapshots 和变更事件，刷新计时器由 `pi-ui` 持有并在能力会话关闭后释放。

`/worker_settings`、`/codex-fast` 是 `pi-ui` 命令，不是能力入口。其他前端可直接使用相应服务，不需要模拟原生菜单。

## 事件与兼容桥

各能力自身的 `frontend.ts` 发布 `pi:frontend-event:v1`，载荷为 `{ context, event }`；事件类型也归该能力所有。`shared/frontend.ts` 与 `frontend-events.ts` 仅作整包兼容与类型聚合。发布不判断前端身份，且观察者故障不能改变业务结果。各前端决定是否展示、文案与通知级别。

为了保持当前 Go UI 和普通 RPC 行为，`pi-ui` 暂时也装配旧 RPC 兼容桥：

- 任意第三方 UI 可用 `PI_UI_PROTOCOL=1` 选择结构化交互；兼容旧 `PI_GOUI_PROTOCOL=1`，问卷另保留 `PI_GOUI_ASK_QUESTION=1`。这些标记仅在适配层读取，原有 `pi-goui:*:v1` wire 名称保持兼容，不代表客户端必须是 Go。
- Go 设置/问卷请求继续使用 `extension_ui_request`，标题 `pi-goui:ask-question:v1`、`pi-goui:worker-settings:v1`、`pi-goui:codex-fast:v1`，placeholder 为 JSON。
- Go 事实事件继续通过 info notify 发送 `pi-goui:event:v1:{"name":...,"data":...}`。
- 原生通知和普通 RPC 菜单回退都在 `pi-ui`，不在能力模块。当前 Go 后端仍须加载此兼容桥；移除整个 `pi-ui` 后不会自动获得这些旧 RPC 交互。

可选通用桥通过标准 Pi RPC 的扩展命令 `pi-ui-rpc` 接收 `{id, method, params}`，以 `pi-ui:rpc:v1:` notify 返回关联结果或错误。Go 必须先用 `get_commands` 确认桥存在且来源为 extension，不能将未知命令发送给模型。

当前方法：`codex-fast.get` / `codex-fast.set` 委托档位能力；`worker.transcript` 委托能力的 `readTranscript`。桥只做路由和传输结构检查，不负责文件读写或业务校验。记录能力验证描述符/路径/游标并限制读取量。缺少能力时返回 unavailable，前端隐藏或降级对应入口，不能回退为直接读取扩展私有文件。

Go 第三方扩展相关实现统一位于 `pi-goui/internal/extensions`：协议和回放在对应能力子包，控件/表单/查看器在 `hostui`，工具卡片/完整详情在 `toolrender`；核心 UI 和 renderer 仅保留通用组件与薄委托。Go 不读写 Codex 能力配置，Worker 私有记录只通过 `worker.transcript` 获取；槽位提示依据稳定 `phaseCode`，不解析中文文案。通用会话浏览、Git/额度展示、编辑器等 UI 辅助功能仍由前端承担。

设置保存事实由能力侧提交成功后发布；UI 订阅并决定通知形式。加载顺序、观察者故障不得影响权威保存结果。

RPC 响应上限按 `notify.message` 再次 JSON 编码后的大小计算（32 MiB 预算，低于接收端 64 MiB 行上限）；超限返回 `response-too-large`，不应断开后端连接。自动命名与 Telegram 使用显式主会话/Worker 策略，不再以 `hasUI` 决定业务执行。

## 文件归属

- `shared/`：服务发现/策略/token helper 的维护模板、兼容事件聚合；无绘制入口，复制安装不依赖它。
- `pi-ui/ask-question/`：问卷、原生工具渲染、Go/通用 RPC 交互。
- `pi-ui/worker/`：进度卡片、刷新、设置菜单和 RPC 交互。
- `pi-ui/codex-fast/`：档位命令、菜单和 RPC 交互。
- `pi-ui/<模块>/`：模块自己的通知文案、原生 UI 与 RPC 适配；`pi-ui/shared/` 仅保留薄 helper 模板与整包兼容 facade。
- `pi-ui/sol-pi.ts`：`obs_recall` / `update_plan` 隐藏卡片策略；能力仍返回完整模型结果。

Go 独立消费 `worker-ui` details：保留状态数据，不传 ANSI 或原生组件。Telegram 是外部集成，不属于本地原生 UI。会话名称属于共享元数据。

调整边界时运行 `pi-ui/architecture.test.ts`、真实加载器集成测试及相关能力/适配测试；同时检查原生 TUI、Go RPC、普通 RPC 和不加载 UI 的路径。
