# Pi Go UI 开发指南

[English](AGENTS_EN.md)

本指南适用于本目录，同时遵守仓库上级 `AGENTS.md`。动手前检查工作区，保留无关修改；未经要求不要提交。

## 项目定位

Pi Go UI 是独立的 Go / Bubble Tea 前端，通过**标准 Pi RPC**启动并连接 Pi 后端。它不嵌入 TypeScript 扩展，不要求特定仓库位置、模型、凭据或作者的个人设置。仓库旁边提供的扩展是**可选第三方集成**，不是 Pi 后端的内置功能。

项目与架构说明统一在 `README.md`。接入扩展前阅读 `internal/extensions/README.md`、`internal/extensions/hostui/README.md` 和 `internal/extensions/toolrender/README.md`。不要再新增独立 DESIGN 文档；给 AI 的修改指导放在本 AGENTS 文档中。中文为默认文档，英文使用同名 `_EN.md`，两者顶部互链。

## 代码归属

以下路径均相对 `pi-goui/`：

| 路径 | 职责 |
| --- | --- |
| `cmd/pi-goui/` | 程序入口及 CLI |
| `internal/rpc/` | 标准 Pi 传输、请求关联、生命周期与通用扩展 RPC 信封 |
| `internal/session/` | 通用会话/消息投影；可选投影通过薄集成文件接入 |
| `internal/ui/` | 宿主 Model、事件循环、通用布局/输入、标签与查看器所有权、薄适配绑定 |
| `internal/render/` | 通用渲染、换行、内容净化、边框、完整值格式化 |
| `internal/extensionapi/` | 小型中立渲染契约，不包含第三方业务 |
| `internal/extensions/<name>/` | 扩展 wire 类型、类型化 RPC 客户端、纯解码与只读投影 |
| `internal/extensions/hostui/` | 扩展表单、控件、查看器、交互及展示状态的**实际实现** |
| `internal/extensions/toolrender/cards/` | 扩展紧凑卡片的**实际实现** |
| `internal/extensions/toolrender/detail/` | 扩展完整工具结果视图的**实际实现** |
| `internal/extensions/dialogs.go`、`notices.go` | 可选协议识别与事实通知展示 |

不要把第三方实现散放在核心 UI/render 包后仅加 `extension_*.go` 前缀。核心现有文件是薄绑定：定位原标签/查看器、传递中立输入、调度 `tea.Cmd`、关闭/聚焦对话框、拒绝迟到结果。扩展行为实现在 `internal/extensions/`。

### 依赖方向

- `hostui` 不得导入 `internal/ui`，也不得接收或复制整个 Model；传递小型状态、回调或所有者句柄。允许复用通用 `internal/render` API。
- `toolrender/cards`、`toolrender/detail` 不得导入父 `internal/render`、`internal/render/tooldetail` 或 `internal/ui`；使用 `extensionapi` 回调，按需复用通用 `render/frame`、`render/tooldata`、`config`、`session`。
- 核心 renderer 的 `extensions.go` 只绑定通用回调与路由，不是另一份扩展实现。
- 回调必须按实例绑定；不得建立可变全局渲染注册表、跨后端共享视图状态，或为搬文件暴露宿主内部类型。
- 通用会话投影不能反向依赖 Worker 回放；Worker 适配可以使用通用会话投影。

## 接入第三方扩展

1. **先确认后端契约。** 阅读扩展自己的 README：发现方式、请求/响应、版本、生命周期、取消与不可用语义。不能从工具名猜接口。本仓库能力各自的 `../extensions/<name>/README.md` 是独立接入指南。
2. **选择最小接口。** 足够时使用标准工具结果、对话框与通知；只在 `internal/extensions/<name>/` 添加必要的专项协议/投影，业务规则留在后端。
3. **展示放入独立目录。** 控件与表单进入 `hostui`，卡片进入 `toolrender/cards`，完整详情进入 `toolrender/detail`。复用通用换行、净化与完整值展示，不复制整个 renderer。
4. **连接薄宿主路由。** 对话框扩展 `internal/extensions/dialogs.go` 识别及 `hostui` 分派，沿用小型 `DialogHost` 桥；完整详情扩展 `detail.Render`，未知工具仍返回 false 并走通用视图；卡片通过 renderer facade 返回通用命中区块。Model 只增加必要的事件/生命周期绑定。
5. **保持可选。** 未安装、未知版本、格式错误、未知载荷和断连不能破坏普通 Pi 交互；隐藏不可用控件，或显示已有结果/普通对话框。不能把看似协议的用户文本当成命令。
6. **补测试与文档。** 覆盖无扩展路径、生命周期及所有权，而非只验证成功专项视图；更新对应 README 及变更的后端指南。

### 权威逻辑属于后端

执行、权限、调度、权威校验、持久化、迁移、记录授权及私有存储归扩展后端。Go 不得读写第三方配置、猜测私有日志路径、从模型名重建设置，或悄悄替代后端执行。

这**不是禁止前端所有本地 I/O**：通用会话浏览、剪贴板/编辑器、图片、Git、额度、更新及前端偏好仍由前端负责。

本仓库 TypeScript 能力支持仅复制 `<模块>/` 与 `ui/<模块>/`（来自 `extensions/pi-ui/<模块>/`），允许使用者自行注册原生 UI。这是后端安装选项，**不是 Go 源码/运行依赖**，不得让 Go 搜索这些目录或读取其文件。

## RPC 与生命周期规则

- 可选方法桥使用 `Client.CallExtension(ctx, method, params)`，先通过 `get_commands` 发现 `name:"pi-ui-rpc", source:"extension"`。不能发送未发现的斜杠命令，否则可能触发模型。
- 桥操作结果是前缀 `pi-ui:rpc:v1:` 的 notify，内层为 `{id,result}` 或 `{id,error:{code,message}}`；外层 prompt 响应不是操作结果。保留普通通知。
- 对话框使用标准 `extension_ui_request` / `extension_ui_response` 并匹配对话 ID；取消是 `cancelled:true`，不是伪造答案或默认同意。
- 历史 `pi-goui:*:v1` 标题与事实前缀 `pi-goui:event:v1:` 是兼容 wire 标识，不要求客户端是 Go。后端支持中立 `PI_UI_PROTOCOL=1`，不要随意重命名 wire 常量。
- 当前方法仅有 `codex-fast.get`、`codex-fast.set`、`worker.transcript`。记录参数使用 **`descriptor`** 和可选后端不透明 cursor；没有公开的 `worker.start`、`worker.cancel` 或远程 Worker 设置方法。`/worker_settings` 是对话命令，`worker` 是 Agent 工具。
- 本地超时/取消仅停止等待，不撤销已保存写入。忽略迟到回复，关闭时释放定时器/监听器，后端退出时清理待处理请求。
- 异步工作捕获原后端、标签/查看器及 revision，不能应用到“当前激活标签”。不要在 goroutine 修改 UI 状态，通过事件循环返回消息/命令。
- 缺少记录时回退已保存任务结果；保留部分记录并标记不完整。没有 end 记录不等于成功。逻辑判断使用 `phaseCode` 等稳定码，不解析翻译文案。
- 后端文本是不可信内容，使用通用安全渲染。除明确需要外，不在用户摘要暴露协议 ID/私有载荷；不自动访问结果里的路径或执行其中的文本。

## 验证

在 `pi-goui/` 内运行，迭代时优先专项检查：

~~~sh
go test ./internal/extensions/... ./internal/render/... ./internal/ui
~~~

跨模块修改交付前：

~~~sh
go test ./...
go vet ./...
GORACE=atexit_sleep_ms=0 go test -race ./... -timeout=90s
git diff --check
~~~

修改的 Go 文件运行 `gofmt`。边界测试约束依赖方向；宿主集成测试覆盖完整输出、ANSI 安全、宽度、折叠、动画与路由，适配器行为测试放在实现旁。异步功能覆盖过期所有者、标签/会话切换、取消与断连；明确测试能力/桥缺失和未知协议降级。

涉及分发/可移植性时，构建二进制，在无关临时项目及独立 HOME、`PI_CODING_AGENT_DIR` 中运行 `--check --no-session`，不复制作者 settings/models/auth。此检查只验证传输启动，不代表真实模型推理或终端交互。

修改配套 TypeScript 时，运行 `npm --prefix ../extensions run typecheck` 及 `npm --prefix ../extensions test`。可复制 helper 以源码随目录提供，维护者用 `npm --prefix ../extensions run sync:portable` 同步模板；使用者不得被要求执行生成步骤。

如实报告验证范围。单元测试不能代表真实终端、跨平台或全仓成功。历史 PTY/SoL-Pi runner 兼容问题与 Go 套件分开归因，不弱化测试或扩大无关改动。
