# Pi Coding Agent 配置

[English](README_EN.md)

这是我的个人 [Pi Coding Agent](https://pi.dev) 配置：自定义扩展、主题、快捷键与工作流，而非通用发行版。仓库只适合存放可公开的配置源码；扩展拥有本机权限，请先审查代码再运行，尤其是第三方扩展。

## 功能概览

- 自定义 TUI：界面、深色主题和 `Ctrl+Y` 会话恢复；页脚显示上下文占用、模型、Thinking 与 Codex Fast 开关。
- `ask_question` 支持单选、多选和多题确认；并发问卷排队显示，避免互相覆盖。`/commit` 生成 Conventional Commit 信息并提交（会暂存全部改动）。
- `/codex-fast` 分别设置 Fast / Ultrafast 请求档位；Thinking 中文翻译与自动中文会话命名默认开启，可在对应配置中关闭。
- [Worker](#worker) 在独立 Pi 子进程中协助完成指定任务；自动路由仅有 Fast、Normal、Deep 三档，Thinking 强度独立设置。路径检查不是安全沙箱，仍须核对修改。
- SoL-Pi 提供长结果回读与计划步骤间的上下文压缩；工具结果过滤只降低常见敏感信息泄露风险，不保证脱敏。

## 本地扩展概览

以下逐项对应当前 `extensions/` 源码，不含 `node_modules`、测试与辅助模块。Pi 自动发现顶层 `.ts` 文件和带 `index.ts` 的目录；SoL-Pi 子组件由其入口注册，`ask-parent.ts` 仅在 Worker 子进程中显式加载，不应重复加载这些内部入口。

| 扩展 / 源码 | 用途 | 入口 | 配置 / 启用条件 |
| --- | --- | --- | --- |
| [ask-question](extensions/ask-question/index.ts) | 排队展示单选、多选、自定义输入或多题问卷 | Agent 工具 `ask_question`，非斜杠命令 | 无独立配置；交互问卷需 TUI，非 TUI 仅返回未回答的问题 |
| [autoname](extensions/autoname/index.ts) | 自动生成/更新中文会话名称，保留用户手动命名 | 回合完全结束后自动运行 | `autoname.json`：`enabled`、`notify`、`cooldownSeconds`、`model`、`reasoning` |
| [codex-fast](extensions/codex-fast/index.ts) | 为符合条件的 Codex 请求设置 Fast / Ultrafast | 仅 `/codex-fast` 原生选择菜单 | `codex-fast.json`：独立布尔开关 `fast`、`ultrafast`，缺省均关闭 |
| [commit](extensions/commit/index.ts) | 暂存全部改动，用当前模型生成英文 Conventional Commit 并立即提交 | `/commit` | 无独立配置；需要 Git、可用模型及认证，无提交前确认菜单 |
| [filter-output](extensions/filter-output/index.ts) | 在模型接收成功的工具结果前过滤常见敏感文本及部分敏感文件读取 | 自动 `tool_result` 钩子，非用户命令 | 无独立配置；不保证脱敏，不过滤错误结果；`.env.example` 读取直接放行 |
| [herdr-agent-state](extensions/herdr-agent-state.ts) | 向 Herdr 的本地 socket 上报工作/阻塞/空闲状态与会话引用 | **内部桥接，非用户命令** | `HERDR_ENV=1` 且存在 `HERDR_SOCKET_PATH`、`HERDR_PANE_ID`；仅绑定有 UI 的主会话，由 Herdr 管理/覆盖 |
| [sol-pi](extensions/sol-pi/index.ts) | 统一注册下面两个 SoL-Pi 子组件 | 自动加载入口，非用户命令 | 无单独配置文件；详见 [SoL-Pi 文档](extensions/sol-pi/README.md) |
| [SoL-Pi / ObservationPack](extensions/sol-pi/extensions/observation-pack/index.ts) | 用归档引用替代重复的大型纯文本工具结果 | 自动上下文投影；工具 `obs_recall` | 由 `sol-pi` 注册；大于 10 KiB、非错误、纯文本结果前两次完整发送，之后引用；本地会话目录或私有临时目录存档 |
| [SoL-Pi / OCC](extensions/sol-pi/extensions/online-context-compact/extension.ts) | 在完成计划步骤后按经济性评估上下文压缩 | 工具 `update_plan` 及回合边界钩子，非用户命令 | 由 `sol-pi` 注册；读取有效 `settings.json` 的 `compaction` / `retry`，项目设置须受信任；需持久化主会话，Worker 禁用 |
| [telegram](extensions/telegram/index.ts) | 向目标聊天发送回合回复与部分提问通知 | 自动事件通知，非用户命令，不提供远程控制 | 环境变量 `PI_TG_TOKEN`、`PI_TG_CHAT`；缺少任一项不发送，无轮询；依赖 `node-telegram-bot-api` |
| [thinking-translation](extensions/thinking-translation/index.ts) | 为短 Thinking 添加中文显示翻译，不改原会话内容或模型上下文 | `/thinking_translation` 切换；实时流自动触发 | `thinking-translation-settings.json`：`enabled`、`model`、`maxThinkingLength`；请求固定 minimal；本地缓存 `~/.pi/thinking-translations/` |
| [ui](extensions/ui/index.ts) | 自定义页头、编辑器、页脚、工具卡片及主 Agent + Worker 工作进度/用量 | 自动 TUI 渲染，非用户命令 | 无独立配置；页脚读取 `codex-fast.json`；主题与显示设置在 `settings.json`，快捷键在 `keybindings.json` |
| [worker](extensions/worker/index.ts) | 有边界的独立子进程任务、并行调度及进度卡片 | Agent 工具 `worker`；`/worker_settings` | `worker-settings.json`：必需的三档模型/Thinking、并发、自动委派、超时及输出上限；此入口在子进程中仅安装写入检查，不可嵌套委派 |
| [Worker / ask-parent](extensions/worker/ask-parent.ts) | Worker 向真正的主 Agent 请求决策 | **内部桥接工具 `ask_parent`，非用户命令** | Worker 显式加载，需子进程深度及 fd 3 控制通道；默认答复超时 120 秒，可选 1–600 秒 |

旧的 `context`、`usage`、`fast` 本地扩展已删除；不再提供这些扩展的 `/context`、`/usage`、`/fast` 命令。上下文占用已在页脚展示，Fast 设置入口为 `/codex-fast`。外部包不在此本地扩展表中。

## 安装

需要 Node.js **22.19+** 与 [Pi Coding Agent](https://github.com/earendil-works/pi)，Git 用于提交和 Worker 的改动检查。SoL-Pi 的 OCC 目前针对 Pi **1.0.2** 验证；升级 Pi 后需重新检查兼容性。

```bash
npm install -g --ignore-scripts @earendil-works/pi-coding-agent
```

先备份已有的个人 agent 目录（若存在）；确认备份目标不存在，以免覆盖。将 `<repository-url>` 换成此仓库地址：

```bash
mv ~/.pi/agent ~/.pi/agent.backup
git clone <repository-url> ~/.pi/agent
npm ci --prefix ~/.pi/agent/extensions
```

启动 Pi：

```bash
pi
```

如果没有现有 `~/.pi/agent`，跳过 `mv`。`npm ci` 安装 `extensions/` 锁文件中的本地依赖（运行时依赖为 `typebox`、`node-telegram-bot-api`，另含 Pi SDK / TypeScript 开发依赖），并不会升级全局 Pi；启动 Pi 后，`settings.json` 声明的 `pi-web-access`、`@ff-labs/pi-fff` 扩展包由 Pi 另行管理。首次使用执行 `/login`；更改扩展、Skill、主题或快捷键后执行 `/reload`。锁文件中的本地 SDK 开发依赖不能替代已验证的全局宿主版本；兼容限制见 [SoL-Pi 文档](extensions/sol-pi/README.md)。

## 常用命令

| 命令 | 用途 |
| --- | --- |
| `/login`、`/model` | 配置认证、选择模型 |
| `/worker_settings` | 调整 Worker 模型与并发等设置 |
| `/commit` | 暂存全部改动、生成提交信息并提交 |
| `/codex-fast` | 打开 Fast / Ultrafast 设置菜单 |
| `/thinking_translation` | 切换 Thinking 中文翻译 |
| `/reload` | 重新加载配置 |

`/login`、`/model`、`/reload` 是 Pi 内置命令，其余见上表本地扩展。`/commit` 会把完整的 staged diff 发给当前模型；使用前确认所有工作区改动都应进入同一次提交。`ask_question`、`worker`、`obs_recall` 与 `update_plan` 是 Agent 工具，不是斜杠命令；其中可选工具受启用条件限制。

### Codex Fast

`/codex-fast` 只打开原生选择菜单，不接受 `on` / `off` 等子命令。上下选择 Fast 或 Ultrafast，**回车切换该项并立即保存**到 `codex-fast.json`，菜单继续显示；**Esc 退出**，不会撤销已保存的切换。菜单保存后立即生效；手动编辑配置文件后需 `/reload`。

仅当 provider 为 `openai-codex`、API 为 `openai-codex-responses` 时修改请求：Ultrafast 开启且 ID **精确等于 `gpt-6-astra`** 时发送 `service_tier: "ultrafast"`；否则仅在 Fast 开启时回退发送 `"priority"`。仅开 Ultrafast 而模型不匹配时不修改请求；这不是服务端拒绝后的重试/降级机制，也不保证服务端支持或加速。

页脚 **✨ 表示 Fast 开关开启，🌟 表示 Ultrafast 开关开启**；两个标记独立，可同时出现。它们表示保存的开关状态，不代表当前模型实际使用了对应请求档位。

### 其他默认行为

这里描述源码缺省值，不复制本地模型或用户设置；本地配置可覆盖缺省值。

- **Autoname** 默认开启并通知，使用当前模型、minimal reasoning，冷却 600 秒；自动命名仅在有 UI 的会话完全结束后处理，保留用户手动名称。
- **Thinking 翻译** 默认开启，长度上限默认 200 个字符，只改显示；配置文件缺失或配置模型不可用时使用当前会话模型。历史恢复只读取已有缓存，不为缺失缓存补发翻译请求；修改模型/长度配置后重新加载。
- **Telegram** 未配置两个环境变量时不发送；只通知，不接收聊天指令。当前提问观察器只识别 `ask_question` 的顶层 `question`，多题 `questions` 问卷不会生成对应提问通知。失败被静默忽略，不保证送达。

## Worker

`worker` 将边界明确、可独立验收的子任务交给独立 Pi 子进程；子进程不持久化会话，不自动加载 Skills 或 Context Files，也不得创建或调用其他 Worker。主 Agent 负责拆分任务、回答问题、检查实际 diff、验证并最终验收。

启动、回答、继续等待和取消统一使用 `worker` 工具。每个主会话同时只允许一个未完成批次；需要并行的任务应放在同一次调用的 `tasks` 数组中。批量结果按输入顺序返回，各项可分别成功、阻塞或失败。

结果摘要和 `changed_files` 是 Worker 报告及工作区快照信息，不替代主 Agent 对真实 diff 的检查。

支持五种 mode：`scout` 只读调查，`implement` 实现，`test` 测试，`review` 只读审查，`fix` 修复已确认的问题。写入模式须声明非空 `allowedPaths`；`relevantFiles` 只是读取提示，不授予写权限。`allowedPaths`/`forbiddenPaths` 相对 `task.cwd`：`backend/file.ts` 精确匹配单个文件（新文件也可），目录后代须用 `backend/**`。既有裸目录 `backend`、`./backend` 或 `backend/` 会在整批启动前报错并建议 `backend/**`；即使目录尚不存在，也应显式使用 `backend/**`，不要依赖裸路径推断。`forbiddenPaths` 排除目录后代也须使用 `/**`。

### 档位与设置

- **Fast**：明确、局部且容易验证的工作。
- **Normal**：常规任务，也是无法判断 Fast 或 Deep 时的默认选择。
- **Deep**：跨模块、根因不明、并发状态等困难任务；它是最高任务复杂度档位。

`preset` 仅接受 `auto`、`fast`、`normal`、`deep`，**没有 Max 执行档位**。Thinking 的 `high`、`xhigh`、`max` 是各档独立的推理强度，并非额外路由档位，必须受模型支持。

各档模型和 Thinking 均从本机 `worker-settings.json` 读取；`fast`、`normal`、`deep` 三项必须配置有效模型，缺失或模型不可用时报错，不自动猜测或降级。加载旧配置时会删除并持久化过期的顶层 `max` 项。源码通用缺省值为并发 3、自动委派开启、任务超时 15 分钟、输出上限 64 KiB。使用 `/worker_settings` 可交互调整这些设置及各档模型/Thinking，选择“保存并退出”后后续任务立即生效；关闭自动委派仅允许以 `manual: true` 显式启动。

### 问答与用户确认

Worker 可通过 `ask_parent` 向真正的主 Agent 提问，不使用额外的协调模型或主会话 fork。`worker` 遇到待答问题时返回，否则等待整批完成，不定时轮询返回。主 Agent 通过同一工具提交一个或多个答案，并继续等待；其他独立任务可同时运行。等待答案的任务仍占用并发槽位与路径锁。

需要用户决策时，主 Agent 将 `ask_question` 作为普通快速调用，再通过原问题 ID 明确将答案回传 Worker，用户选择不会自动转发。**任务和问题的普通超时始终继续，涵盖等待用户确认和问卷排队**，不额外预留或延长时间；独立任务继续运行。超时后的迟到答复会被拒绝，不能使任务或问题恢复。并发问卷按顺序展示，取消和会话结束会清理界面及队列。

取消或退出确认不代表同意。答案不能扩大原任务权限；需要扩大范围时，应先取消、等待清理，再创建新任务。若决策影响正在执行的危险操作，应先取消相关批次，不能把人工等待当作暂停任意运行工具。

### 并行、进度与验收

批量任务在并发上限内调度：只读任务可并行；读写任务冲突，必须串行；写入任务仅在 `cwd` 与全部声明写路径可解析且确认互不重叠时并行。无法证明独立时按冲突处理，且调度不保证独立 Git worktree。同进程各会话的子进程、槽位和关闭清理独立；不同 Pi 窗口即使控制通道隔离，仍可能共享工作树，路径锁不跨窗口。

每个批次只显示最初一张 Worker 卡片，复用 `extensions/ui/` 的统一工具样式；后续回答、等待和取消调用仍保留在模型记录中，但不新增可见卡片。任务状态、工具活动、档位、轮次、用量、耗时、完成摘要及各任务完整问答均在原卡片默认显示。不再显示 Batch 编号汇总和正常的执行槽位等待提示；错误与超时诊断仍保留。

进度内容经过截断和常见敏感字段过滤，不是完整脱敏保证。取消、超时或输出超限会尝试终止子进程；取消调用会等待清理完成。失败交由主 Agent 检查处理，不自动重试。重载、会话切换或树导航会清理后台任务；历史卡片使用当前分支最新的持久化快照，不恢复子进程。

`cwd` 必须位于主工作目录内；`allowedPaths`/`forbiddenPaths` 只在 `edit`、`write` 工具调用前检查。它们不是 shell 或文件系统沙箱，不能限制 `bash` 或其他扩展工具；不要把它们视为隔离边界。主 Agent 必须检查实际修改、原有工作区改动、验证证据和任务范围，再决定是否验收。

待答时意外断管会明确失败并清理，不把子进程捕获 IPC 错误后的“完成”视为成功，也不自动重连或重派。失败诊断保留终止来源及必要进程标识；旧记录无法据此反推取消原因。自定义 SDK 宿主须发送并等待 `session_shutdown`；Pi 0.87.1 的裸 `AgentSession.dispose()` 不发送该事件。协议、宿主限制及测试命令见 [Worker 扩展文档](extensions/worker/README.md)。

## 配置与安全

- `settings.json` 声明 Pi 扩展包和主题/界面等设置；`worker-settings.json`、`codex-fast.json`、`autoname.json` 与 `thinking-translation-settings.json` 控制对应功能。`keybindings.json` 配置 `Ctrl+Y` 会话恢复；主题源码在 [themes/pi.json](themes/pi.json)。
- 登录凭据与模型密钥（如 `auth.json`、`models.json`、`models-store.json`）、会话（如 `sessions/`）应保留在被忽略的本地文件中，不要提交或复制到公开仓库。
- 自定义模型配置应在本地创建，并优先通过环境变量引用密钥；参见 [模型配置](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/models.md)。
- `.gitignore` 不会保护已跟踪的文件；发布前检查 `git status --short --ignored` 和 `git diff --cached`。若凭据已泄露，应移除公开内容并轮换密钥。
- Telegram 通知默认不使用；如需启用，先审查扩展源码并安全配置聊天目标与 Bot Token，**不要将凭据写入仓库**。通知可能把任务输入、回复和提问发往目标聊天，且不保证送达。
- Thinking 翻译和自动会话命名可能向各自使用的模型发送 Thinking 或会话内容，产生额外请求与费用；使用前确认接收方。本地翻译缓存、SoL-Pi 归档和 Herdr 会话引用同样应按敏感数据处理。
- 工具结果过滤仅为启发式处理，可能遗漏秘密或误遮盖普通内容；它不能代替凭据管理、提交前审查或限制扩展的本机权限。

## SoL-Pi

`extensions/sol-pi/` 提供两项机制；文件修改与后续验证由 codemode 串联原生工具：

1. **ObservationPack**：将重复的大型纯文本结果换成可用 `obs_recall` 按字节偏移分页回读的引用；归档保留在本地会话目录（无持久会话时使用私有临时目录），不自动清理或脱敏。
2. **Online Context Compact（OCC）**：`update_plan` 每次替换完整计划；仅当前回合成功更新计划、完成先前登记的未完成步骤且仍有剩余工作时，才可能在经济性评估通过后压缩并继续任务，完成整个计划不会触发。需持久化主会话且有效 `compaction.enabled` 开启；摘要可能丢失细节，且会产生额外模型请求与费用，并非省钱保证。OCC 针对 Pi **1.0.2**，在 Worker 中禁用，ObservationPack 不因此禁用。

OCC 的边界压缩不触发原生 `session_before_compact` / `session_compact` 钩子；依赖这些钩子拦截所有压缩的工作流应关闭 OCC/压缩。

来源、配置、兼容限制及离线测试命令见 [SoL-Pi 扩展文档](extensions/sol-pi/README.md)。
