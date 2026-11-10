# Pi Coding Agent 配置

[English](README_EN.md)

这是我的个人 [Pi Coding Agent](https://pi.dev) 配置与独立终端前端：包含 `pi-goui`、自定义扩展、主题、快捷键与工作流，而非通用发行版。仓库只适合存放可公开的配置源码；扩展拥有本机权限，请先审查代码再运行，尤其是第三方扩展。

## 功能概览

- **[pi-goui](pi-goui/README.md)**：Go / Bubble Tea v2 独立前端，每个会话使用独立 `pi --mode rpc` 子进程，复用 Pi 的 Agent、工具、认证、配置和扩展。侧栏展示会话、目录、CTX、用量和符合条件的 Codex 额度；背景默认透明。
- 新建空会话可点击侧栏目录设置 cwd，支持目录候选、Tab 补全和逐级浏览；各会话独立保存草稿、图片、队列、滚动及折叠状态，切换不打断后台任务。
- `ask_question` 支持单选、多选和多题确认；并发问卷排队显示，避免互相覆盖。
- `/codex-fast` 分别设置 Fast / Ultrafast 请求档位；自动中文会话命名默认开启，可在对应配置中关闭。
- [Worker](#worker) 在独立 Pi 子进程中协助完成指定任务；主模型显式选择 Fast、Normal、Deep 三档，Thinking 强度独立设置。路径检查不是安全沙箱，仍须核对修改。
- [SoL-Pi](#上下文管理sol-pi) 提供大工具结果归档与在线上下文压缩；工具结果过滤只降低常见敏感信息泄露风险，不保证脱敏。

## 本地扩展概览

以下逐项对应当前 `extensions/` 源码，不含 `node_modules`、测试与辅助模块。Pi 自动发现顶层 `.ts` 文件和带 `index.ts` 的目录，不应重复加载内部入口。

| 扩展 / 源码 | 用途 | 入口 | 配置 / 启用条件 |
| --- | --- | --- | --- |
| [ask-question](extensions/ask-question/index.ts) | 排队展示单选、多选、自定义输入或多题问卷 | Agent 工具 `ask_question`，非斜杠命令 | 无独立配置；TUI 使用完整自定义问卷界面，RPC/Pi Web 使用标准对话框支持单选、自定义输入、顺序多题和模拟多选；无 UI 模式仅返回未回答的问题 |
| [autoname](extensions/autoname/index.ts) | 自动生成/更新中文会话名称，保留用户手动命名 | 回合完全结束后自动运行 | `autoname.json`：`enabled`、`notify`、`cooldownSeconds`、`model`、`reasoning` |
| [codex-fast](extensions/codex-fast/index.ts) | 为符合条件的 Codex 请求设置 Fast / Ultrafast | 仅 `/codex-fast` 标准选择菜单 | `codex-fast.json`：独立布尔开关 `fast`、`ultrafast`，缺省均关闭 |
| [filter-output](extensions/filter-output/index.ts) | 在模型接收成功的工具结果前过滤常见敏感文本及部分敏感文件读取 | 自动 `tool_result` 钩子，非用户命令 | 无独立配置；不保证脱敏，不过滤错误结果；`.env.example` 读取直接放行 |
| [herdr-agent-state](extensions/herdr-agent-state.ts) | 向 Herdr 的本地 socket 上报工作/阻塞/空闲状态与会话引用 | **内部桥接，非用户命令** | `HERDR_ENV=1` 且存在 `HERDR_SOCKET_PATH`、`HERDR_PANE_ID`；仅绑定有 UI 的主会话，由 Herdr 管理/覆盖 |
| [sol-pi](extensions/sol-pi/index.ts) | ObservationPack 大工具结果归档、Online Context Compact 在线上下文压缩 | 自动上下文钩子；Agent 工具 `obs_recall`、`update_plan` | 自动发现入口，不要重复加载；详见 [SoL-Pi 文档](extensions/sol-pi/README.md) |
| [telegram](extensions/telegram/index.ts) | 向目标聊天发送回合回复与部分提问通知 | 自动事件通知，非用户命令，不提供远程控制 | 环境变量 `PI_TG_TOKEN`、`PI_TG_CHAT`；缺少任一项不发送，无轮询；依赖 `node-telegram-bot-api` |
| [worker](extensions/worker/index.ts) | 有边界的独立子进程任务、并行调度及进度卡片 | Agent 工具 `worker`；`/worker_settings` | `worker-settings.json`：必需的三档模型/Thinking、并发、超时及输出上限；此入口在子进程中仅安装写入检查，不可嵌套委派 |

旧原生 UI 已从 `extensions/ui/` 原样归档到根目录 [`ui/`](ui/README.md)，**不再自动加载**。活动扩展使用 [`extensions/shared/`](extensions/shared/README.md) 中的共享绘制和 token 辅助函数；该目录没有扩展入口，不会单独加载。归档保留原相对导入，恢复原生 UI 时须移回 `extensions/ui/`；不要直接加载根归档。移动文件不会卸载运行中进程的补丁，需要重启 Pi / `pi-goui`。

`btw` 扩展已删除，不再提供 `/btw` 或 `Ctrl+B` 临时问答。旧的 `context`、`usage`、`fast` 本地扩展已删除；不再提供这些扩展的 `/context`、`/usage`、`/fast` 命令。`pi-goui` 在侧栏展示上下文占用，Fast 设置入口为 `/codex-fast`。`rolling-compact` 及其配置已移除，`/roll-compact` 不再提供；上下文管理恢复使用 SoL-Pi。外部包不在此本地扩展表中。

## 安装

需要 Node.js **22.19+** 与 [Pi Coding Agent](https://github.com/earendil-works/pi)；使用 `pi-goui` 还需要 **Go 1.26+**。Git 用于 Worker 改动检查及侧栏只读仓库状态。

```bash
npm install -g --ignore-scripts @earendil-works/pi-coding-agent
```

先备份已有的个人 agent 目录（若存在）；确认备份目标不存在，以免覆盖。将 `<repository-url>` 换成此仓库地址：

```bash
mv ~/.pi/agent ~/.pi/agent.backup
git clone <repository-url> ~/.pi/agent
npm ci --prefix ~/.pi/agent/extensions
```

首次使用先在原生 Pi 中执行 `/login` 完成认证：

```bash
pi
```

随后从工作项目目录启动 Go 前端：

```bash
~/.pi/agent/pi-goui/run.sh                         # 使用调用者的当前目录
~/.pi/agent/pi-goui/run.sh --cwd /path/to/project
~/.pi/agent/pi-goui/run.sh -c                      # 恢复该目录最近的会话
~/.pi/agent/pi-goui/run.sh --session /path/to/session.jsonl
~/.pi/agent/pi-goui/run.sh --check                 # 只读 RPC 检查，不提交提示词
```

`run.sh` 每次构建 `pi-goui/bin/pi-goui` 后启动；`PI_BIN` 或 `--pi` 指定 Pi 可执行文件，`--no-session` 禁止保存会话。原生 `pi` 仍可独立使用，但不再自动加载旧 UI。

如果没有现有 `~/.pi/agent`，跳过 `mv`。`npm ci` 安装 `extensions/` 锁文件中的本地依赖（运行时依赖为 `typebox`、`node-telegram-bot-api`，另含 Pi SDK / TypeScript 开发依赖），并不会升级全局 Pi；启动 Pi 后，`settings.json` 声明的 `pi-web-access` 扩展包由 Pi 另行管理。原生 Pi 可通过 `/reload` 重载扩展、Skill、主题和快捷键；`pi-goui` 的 `/reload` 只重读前端配置，更新扩展或前端代码后请重启前端，以启动新的 RPC 后端。锁文件中的本地 SDK 开发依赖不能替代已验证的全局宿主版本。SoL-Pi 已在全局 Pi **1.0.4** 上验证，隔离测试入口也限定该版本；详见 [SoL-Pi 验证说明](extensions/sol-pi/README.md#offline-validation)。

### 更新检查与变更内容

启动时在后台检查 Pi 和用户级外部包，有更新则在聊天区默认欢迎内容下方提示；**仅通知，不弹窗、不执行升级**。Pi 更新后的首次启动会在同一区域展示安装附带的当前版本变更内容，可滚动阅读；首次使用也展示一次，后续启动不重复。扩展不展示变更内容。

检查失败或没有更新时保持安静。离线模式跳过联网检查，但仍可展示本地 Pi 变更内容；只在前端独立缓存中记录已展示版本，不改 Pi 设置或聊天历史。详见 [启动更新通知](pi-goui/README.md#启动更新通知与-pi-变更内容)。

## 常用命令

| 命令 | 用途 |
| --- | --- |
| `/login` | 仅在原生 Pi 中配置认证；前端复用已有登录 |
| `/model`、`/thinking` | 在 `pi-goui` 中选择模型、思考强度 |
| `/new`、`/sessions`、`/resume` | 新建独立会话、选择或恢复历史 |
| `/name` | 设置当前会话名称 |
| `/attach path`、`/detach` | 插入图片引用、清除待发送图片 |
| `/hotkeys` | 查看实际生效的快捷键 |
| `/worker_settings` | 调整 Worker 模型与并发等设置 |
| `/codex-fast` | 打开 Fast / Ultrafast 设置菜单 |
| `/reload` | 在 `pi-goui` 中重读前端配置；原生 Pi 的重载范围不同 |
| `/close`、`/quit` | 关闭当前会话、退出前端并回收子进程 |

表中会话、输入和显示命令由 `pi-goui` 处理，扩展设置命令交给 Pi RPC 后端。`/login`、`/logout`、完整原生设置界面和依赖 TypeScript `custom()` 的复杂 TUI 不保证 RPC 可用，请使用原生 Pi；标准 select、confirm、input、editor 对话框受支持。`ask_question`、`worker`、`obs_recall` 与 `update_plan` 是 Agent 工具，不是斜杠命令。

### 界面与问卷交互

以下描述当前 `pi-goui`，不是已归档的原生 UI：

- **侧栏与输入**：SESSION 显示名称、cwd 和 CTX，STATS 显示 `in/out/cache/cost`；`+ New session` 新建会话。紫色输入框底栏包含快捷键、可点击模型和思考强度；working 只在运行中显示，完成指标保留到新输入。
- **设置 cwd**：仅新建且没有消息、排队或运行任务的会话可点击 SESSION 目录。弹窗支持绝对路径、相对当前 cwd 的路径和 `~/`；自动列出目录，↑↓ 选择、Tab 补全，点击目录进入、点击 `../` 返回上级。Enter 确认**输入框中的目录**，Esc 取消。非法路径保留弹窗并提示错误；有效变更只替换该会话的后端，保留未发送草稿与图片，不修改全局 cwd 或其他会话。
- **消息与工具**：用户、助手、系统使用不同渐变边框；标题为「你」、当前模型名称/ID 和「系统」。所有工具调用均入组；尚无后续说明时自动展示最近三条，并视为展开，点击组本身全部收起，再点击完整展开。手动折叠优先；运行中的 Worker 独立展示，结束后合回相邻组。`Ctrl+O` 切换工具展开，`Ctrl+T` 切换思考可见性。
- **会话隔离**：每个标签使用独立后端和界面状态。恢复历史原位替换标签，有草稿或运行任务时先确认，已打开的目标只激活。会话选择器中 `Ctrl+R` 重命名、`Ctrl+D` 确认删除；删除会先关闭对应写入进程。进程隔离不是文件系统沙箱。
- **输入与图片**：`/` 模糊命令补全，`@file` 检索并插入路径。图片粘贴或 `/attach` 在光标处插入蓝色 `[image1]` 等标记，与文字同条发送；删除标记就不会发送该图片。发送后点击标记使用系统查看器打开经校验的私有临时副本，不重新读取源文件、不上传；Linux/macOS 支持查看，退出前端清理临时文件。
- **弹窗与问卷**：弹窗水平居中、下沿贴输入框，背景聊天仍可见；问卷排队显示，RPC 标准对话框与原生自定义界面不完全相同。原生 UI 的组件不能直接复用于 Go 前端。
- **快捷键**：遵循原生动作名及 `keybindings.json` 覆盖，`[]` 禁用对应动作；本仓库 `Ctrl+Y` 绑定会话恢复。Esc 中止当前任务，Shift+Tab 循环思考强度；仅显式按 `Ctrl+S` 才保存默认思考强度。

完整使用说明与 RPC 边界见 [pi-goui 文档](pi-goui/README.md)。

### Codex Fast

`/codex-fast` 只打开标准选择菜单（原生 Pi 与 RPC 前端均可使用），不接受 `on` / `off` 等子命令。上下选择 Fast 或 Ultrafast，**回车切换该项并立即保存**到 `codex-fast.json`，菜单继续显示；**Esc 退出**，不会撤销已保存的切换。菜单保存后立即生效；手动编辑配置文件后，在原生 Pi 中使用 `/reload`，或重启 `pi-goui` 的后端。

仅当 provider 为 `openai-codex`、API 为 `openai-codex-responses` 时修改请求：Ultrafast 开启且 ID **精确等于 `gpt-6-astra`** 时发送 `service_tier: "ultrafast"`；否则仅在 Fast 开启时回退发送 `"priority"`。仅开 Ultrafast 而模型不匹配时不修改请求；这不是服务端拒绝后的重试/降级机制，也不保证服务端支持或加速。

旧原生 UI 页脚的 ✨ / 🌟 开关标记属于已归档实现，`pi-goui` 不展示这些标记。是否启用以 `/codex-fast` 菜单或保存的配置为准，不应从模型名称或前端状态推断请求档位。

### Codex 订阅余量

`pi-goui` 在侧栏显示剩余额度百分比、重置倒计时和整数 Credits，不再使用旧页脚徽标。仅当前模型使用官方 `openai-codex` 来源、且已有可用 OAuth 登录时查询；自定义来源和 API key 不会触发额度请求。

前端只读已有认证，通过固定的 `https://chatgpt.com/backend-api/wham/usage` 查询，约每分钟异步刷新；不复制或刷新凭据，不使用代理、不跟随重定向。失效登录需由 Pi 处理；不可用字段显示占位值，不伪造额度。

### 其他默认行为

这里描述源码缺省值，不复制本地模型或用户设置；本地配置可覆盖缺省值。

- **Autoname** 默认开启并通知，使用当前模型、minimal reasoning，冷却 600 秒；自动命名仅在有 UI 的会话完全结束后处理，保留用户手动名称。
- **Telegram** 未配置两个环境变量时不发送；只通知，不接收聊天指令。当前提问观察器只识别 `ask_question` 的顶层 `question`，多题 `questions` 问卷不会生成对应提问通知。失败被静默忽略，不保证送达。

## Worker

`worker` 将边界明确、可独立验收的子任务交给独立 Pi 子进程；每项任务必须显式填写 `sessionMode` 和 `preset`，主模型优先选择 `fork`（当前活动分支的隔离快照），仅在上下文交接很少时使用 `new`（独立新会话），子会话仅临时保存，不自动加载 Skills 或 Context Files，也不得创建或调用其他 Worker。主 Agent 负责拆分任务、处理阻塞、检查实际 diff、验证并最终验收。

启动、继续等待和取消统一使用 `worker` 工具。每个主会话同时只允许一个未完成批次；所有任务统一传入 `tasks` 数组，单任务也不例外；并行任务应在同一次调用中提交。批量结果按输入顺序返回，各项可分别成功、阻塞或失败。

结果摘要和 `changed_files` 是 Worker 报告及工作区快照信息，不替代主 Agent 对真实 diff 的检查。

支持五种 mode：`scout` 只读调查，`implement` 实现，`test` 测试，`review` 只读审查，`fix` 修复已确认的问题。写入模式须声明非空 `allowedPaths`；`relevantFiles` 只是读取提示，不授予写权限。`allowedPaths`/`forbiddenPaths` 相对 `task.cwd`：`backend/file.ts` 精确匹配单个文件（新文件也可），目录后代须用 `backend/**`。既有裸目录 `backend`、`./backend` 或 `backend/` 会在整批启动前报错并建议 `backend/**`；即使目录尚不存在，也应显式使用 `backend/**`，不要依赖裸路径推断。`forbiddenPaths` 排除目录后代也须使用 `/**`。

### 档位与设置

- **Fast**：明确、局部且容易验证的工作。
- **Normal**：常规任务，也是无法判断 Fast 或 Deep 时的默认选择。
- **Deep**：跨模块、根因不明、并发状态等困难任务；它是最高任务复杂度档位。

`preset` 必填，仅接受 `fast`、`normal`、`deep`，**没有 auto 或 Max 执行档位**，不按关键词或文件数推断。Thinking 的 `off/minimal/low/medium/high/xhigh/max` 是各档独立的推理强度，并非额外路由档位，必须受模型支持。

各档模型和 Thinking 均从本机 `worker-settings.json` 按原值读取，不自动转换或升降级；`fast`、`normal`、`deep` 三项必须配置有效模型，缺失或模型不可用时报错，不自动猜测或降级。加载旧配置时会删除并持久化过期的顶层 `max` 和 `automaticDelegationEnabled` 项。委派始终受支持，无需开关或 `manual` 参数。源码通用缺省值为并发 3、任务超时 15 分钟、输出上限 64 KiB。使用 `/worker_settings` 可交互调整这些设置及各档模型/Thinking，选择“保存并退出”后后续任务立即生效。

### 阻塞与用户决策

`worker` 仅支持启动、等待完成和取消，不提供子任务交互问答。子任务缺少信息、决策或权限时直接返回 `blocked` 并释放资源，由主 Agent 处理；必要时主 Agent 使用 `ask_question` 向用户确认，再创建边界明确的新任务。取消不代表授权，扩大范围前应取消相关批次并等待清理。任务超时不因等待用户而延长。

### 并行、进度与验收

批量任务在并发上限内调度：只读任务可并行；读写任务冲突，必须串行；写入任务仅在 `cwd` 与全部声明写路径可解析且确认互不重叠时并行。无法证明独立时按冲突处理，且调度不保证独立 Git worktree。同进程各会话的子进程、槽位和关闭清理独立；不同 Pi 窗口相互独立，仍可能共享工作树，路径锁不跨窗口。

`pi-goui` 根据真实 Worker details 展示任务目标、状态、模型、session mode、用量、耗时、完成摘要与最近活动，支持任务级折叠；运行中默认展开，完成后归入工具组，手动折叠优先。它自行渲染 RPC 数据，不加载归档 UI；活动 TypeScript 扩展的共享绘制依赖位于 `extensions/shared/`。

进度内容经过截断和常见敏感字段过滤，不是完整脱敏保证。取消、超时或输出超限会尝试终止子进程；取消调用会等待清理完成。失败交由主 Agent 检查处理，不自动重试。同一 Pi 后端内的重载、会话切换或树导航会清理后台任务；`pi-goui` 仅切换前端标签不会触发该后端生命周期，后台会话继续运行。历史卡片使用当前分支持久化快照，不恢复子进程。

`cwd` 必须位于主工作目录内；`allowedPaths`/`forbiddenPaths` 只在 `edit`、`write` 工具调用前检查。它们不是 shell 或文件系统沙箱，不能限制 `bash` 或其他扩展工具；不要把它们视为隔离边界。主 Agent 必须检查实际修改、原有工作区改动、验证证据和任务范围，再决定是否验收。

失败诊断保留终止来源及必要进程标识；旧记录无法据此反推取消原因。自定义 SDK 宿主须发送并等待 `session_shutdown`；Pi 0.87.1 的裸 `AgentSession.dispose()` 不发送该事件。生命周期、宿主限制及测试命令见 [Worker 扩展文档](extensions/worker/README.md)。

## 配置与安全

- `settings.json` 声明 Pi 扩展包和主题/界面等设置；`worker-settings.json`、`codex-fast.json` 与 `autoname.json` 控制对应功能。`keybindings.json` 配置 `Ctrl+Y` 会话恢复；主题源码在 [themes/pi.json](themes/pi.json)。
- 登录凭据与模型密钥（如 `auth.json`、`models.json`、`models-store.json`）、会话（如 `sessions/`）应保留在被忽略的本地文件中，不要提交或复制到公开仓库。
- 自定义模型配置应在本地创建，并优先通过环境变量引用密钥；参见 [模型配置](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/models.md)。
- `.gitignore` 不会保护已跟踪的文件；发布前检查 `git status --short --ignored` 和 `git diff --cached`。若凭据已泄露，应移除公开内容并轮换密钥。
- Telegram 通知默认不使用；如需启用，先审查扩展源码并安全配置聊天目标与 Bot Token，**不要将凭据写入仓库**。通知可能把任务输入、回复和提问发往目标聊天，且不保证送达。
- 自动会话命名可能向使用的模型发送会话内容，产生额外请求与费用；使用前确认接收方。Herdr 会话引用同样应按敏感数据处理。
- 工具结果过滤仅为启发式处理，可能遗漏秘密或误遮盖普通内容；它不能代替凭据管理、提交前审查或限制扩展的本机权限。

## 上下文管理：SoL-Pi

[SoL-Pi](extensions/sol-pi/README.md) 通过 `extensions/sol-pi/index.ts` 自动加载；不要再显式加载其内部入口。本地版本仅包含 ObservationPack 与 Online Context Compact，不包含 Action Fusion、Reducer 或上游全局配置加载器。

- **ObservationPack**：超过 10 KiB 的纯文本、非错误工具结果先在两次 provider 请求中保留全文，之后替换为稳定 ID 与首尾摘要引用；模型可用 `obs_recall` 分页取回原文。短文本、错误、混合媒体及 recall 结果不打包，不重写原始会话历史。
- **在线压缩**：仅在持久化主会话中运行，Worker 中禁用。`update_plan` 须提交完整计划；完成先前登记的未完成步骤、仍有剩余工作且通过经济性评估时，才可能压缩并续接任务。完成整个计划不会触发；摘要可能丢失细节并产生额外模型费用，不保证省钱。它不是旧的 `/roll-compact` 手动摘要流程。工具调用由当前前端渲染，不依赖旧 UI 的隐藏补丁；不显示宣传横幅或节省提示。
- **本地存档**：大结果保存在会话目录下的 `sol-pi/<session-id>/observation-pack/`。无持久会话时使用临时目录，退出后仍保留，但进程重启不恢复映射；这些原文应按敏感数据处理，不会自动删除。

验证命令与版本限制见 [SoL-Pi 文档](extensions/sol-pi/README.md#offline-validation)。

## 前端验证

在 `pi-goui/` 中运行：

```bash
go test -race ./...
go vet ./...
go build -trimpath -o bin/pi-goui ./cmd/pi-goui
python3 script/test-ui.py --binary bin/pi-goui
```

默认使用假 RPC 与隔离 PTY 验证，不调用收费模型。可选真实 RPC 检查通过只读白名单代理执行，详见 [pi-goui 验证说明](pi-goui/README.md#验证)。扩展的验证命令与宿主版本约束见各自文档。
