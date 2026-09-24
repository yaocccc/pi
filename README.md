# Pi Coding Agent 配置

[English](README_EN.md)

这是我的个人 [Pi Coding Agent](https://pi.dev) 配置：自定义扩展、主题、快捷键与工作流，而非通用发行版。仓库只适合存放可公开的配置源码；扩展拥有本机权限，请先审查代码再运行，尤其是第三方扩展。

## 功能概览

- 自定义 TUI：界面、深色主题和 `Ctrl+Y` 会话恢复。
- `/context` 查看上下文占用；`/usage` 查看 Codex 用量。
- `ask_question` 支持单选、多选和多题确认；并发问卷排队显示，避免互相覆盖。`/commit` 生成 Conventional Commit 信息并提交（会暂存全部改动）。
- `/fast` 为符合条件的 Codex 请求尝试 priority tier；Thinking 翻译和自动中文会话命名可按需使用。
- `/impeccable` 选择设计指令并插入可编辑提示词；本仓库不附带对应 Skill。
- Indexed Memory 按需检索本地记忆，也可用 `/summarize` 请求总结；记忆内容不随仓库分发。
- [Worker](#worker) 在独立 Pi 子进程中协助完成指定任务；Fast、Normal、Deep 可自动选择，**Max 仅在用户明确要求时使用**。路径检查不是安全沙箱，仍须核对修改。
- SoL-Pi 提供文件修改后验证、长结果回读与计划步骤间的上下文压缩；工具结果过滤只降低常见敏感信息泄露风险，不保证脱敏。

## 安装

需要 Node.js **22.19+** 与 [Pi Coding Agent](https://github.com/earendil-works/pi)。SoL-Pi 的 OCC 目前针对 Pi **0.87.1** 验证；升级 Pi 后需重新检查兼容性。

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

如果没有现有 `~/.pi/agent`，跳过 `mv`。`npm ci` 只安装 `extensions/` 锁文件中的本地依赖；启动 Pi 后，`settings.json` 声明的 `pi-web-access`、`@ff-labs/pi-fff` 扩展包由 Pi 另行管理。首次使用执行 `/login`；更改扩展、Skill、主题或快捷键后执行 `/reload`。

## 常用命令

| 命令 | 用途 |
| --- | --- |
| `/login`、`/model` | 配置认证、选择模型 |
| `/context`、`/usage` | 查看上下文占用、Codex 用量 |
| `/summarize`、`/memory_settings` | 保存可复用记忆、调整记忆设置 |
| `/worker_settings` | 调整 Worker 模型与并发等设置 |
| `/commit` | 暂存全部改动、生成提交信息并提交 |
| `/fast`、`/thinking_translation` | 切换可选的 priority tier、Thinking 翻译 |
| `/impeccable`、`/reload` | 选择设计指令、重新加载配置 |

`/commit` 会把完整的 staged diff 发给当前模型；使用前确认所有工作区改动都应进入同一次提交。`memory_search`、`memory_get`、`ask_question` 和 `worker` 等是 Agent 工具，无需手动执行。

## Worker

`worker` 将边界明确、可独立验收的子任务交给独立 Pi 子进程；子进程不持久化会话，不自动加载 Skills 或 Context Files，也不得创建或调用其他 Worker。主 Agent 负责拆分任务、回答问题、检查实际 diff、验证并最终验收。

启动、回答、继续等待和取消统一使用 `worker` 工具。每个主会话同时只允许一个未完成批次；需要并行的任务应放在同一次调用的 `tasks` 数组中。批量结果按输入顺序返回，各项可分别成功、阻塞或失败。

结果摘要和 `changed_files` 是 Worker 报告及工作区快照信息，不替代主 Agent 对真实 diff 的检查。

支持五种 mode：`scout` 只读调查，`implement` 实现，`test` 测试，`review` 只读审查，`fix` 修复已确认的问题。写入模式须声明非空 `allowedPaths`；`relevantFiles` 只是读取提示，不授予写权限。

### 档位与设置

- **Fast**：明确、局部且容易验证的工作。
- **Normal**：常规任务，也是无法判断 Fast 或 Deep 时的默认选择。
- **Deep**：跨模块、根因不明、并发状态等困难任务；它是自动路由的最高复杂度档位。
- **Max**：不是自动路由档位；只有用户明确要求 Max、最高强度或同等表述时才可使用，不能静默降级。

各档模型和 Thinking 均从本机 `worker-settings.json` 读取。使用 `/worker_settings` 可交互调整模型、Thinking、并发、自动委派、超时和输出上限；保存后续任务立即生效。配置的模型必须可用。

### 问答与用户确认

Worker 可通过 `ask_parent` 向真正的主 Agent 提问，不使用额外的协调模型或主会话 fork。`worker` 遇到待答问题时返回，否则等待整批完成，不定时轮询返回。主 Agent 通过同一工具提交一个或多个答案，并继续等待；其他独立任务可同时运行。等待答案的任务仍占用并发槽位与路径锁。

需要用户决策时，主 Agent 调用 `ask_question`，再明确将答案回传 Worker，用户选择不会自动转发。实际确认界面打开期间，仅待答任务的剩余任务预算和问题超时暂停；新提出的问题也会加入暂停，独立任务继续运行。人工等待有 **30 分钟上限**，正常结束后恢复剩余预算，不重置为完整时长；超出上限按超时或取消处理，不视为批准。排队中的问卷不会额外触发暂停，其排队时间也计入交互上限。

取消或退出确认不代表同意。答案不能扩大原任务权限；需要扩大范围时，应先取消、等待清理，再创建新任务。若决策影响正在执行的危险操作，应先取消相关批次，不能把人工等待当作暂停任意运行工具。

### 并行、进度与验收

批量任务在并发上限内调度：只读任务可并行；读写任务冲突，必须串行；写入任务仅在 `cwd` 与全部声明写路径可解析且确认互不重叠时并行。无法证明独立时按冲突处理，且调度不保证独立 Git worktree。

每个批次只显示最初一张 Worker 卡片，复用 `extensions/ui/` 的统一工具样式；后续回答、等待和取消调用仍保留在模型记录中，但不新增可见卡片。任务状态、工具活动、档位、轮次、用量、耗时、完成摘要及各任务问答在原卡片更新，展开可查看完整问答。不再显示 Batch 编号汇总和正常的执行槽位等待提示；错误与超时诊断仍保留。

进度内容经过截断和常见敏感字段过滤，不是完整脱敏保证。取消、超时或输出超限会尝试终止子进程；取消调用会等待清理完成。失败交由主 Agent 检查处理，不自动重试。重载、会话切换或树导航会清理后台任务；历史卡片使用当前分支最新的持久化快照，不恢复子进程。

`cwd` 必须位于主工作目录内；`allowedPaths`/`forbiddenPaths` 只在 `edit`、`write` 工具调用前检查。它们不是 shell 或文件系统沙箱，不能限制 `bash`、其他扩展工具或 `then_run` 命令；不要把它们视为隔离边界。主 Agent 必须检查实际修改、原有工作区改动、验证证据和任务范围，再决定是否验收。

协议、限制及测试命令见 [Worker 扩展文档](extensions/worker/README.md)。

## 配置与安全

- `settings.json` 声明 Pi 扩展包；`worker-settings.json`、`memory-settings.json`、`fast.json`、`autoname.json` 与 `thinking-translation-settings.json` 控制对应功能。
- 登录凭据与模型密钥（如 `auth.json`、`models.json`、`models-store.json`）、会话和记忆（如 `sessions/`、`memory-index.md`、`memories/`）应保留在被忽略的本地文件中，不要提交或复制到公开仓库。
- 自定义模型配置应在本地创建，并优先通过环境变量引用密钥；参见 [模型配置](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/models.md)。
- `.gitignore` 不会保护已跟踪的文件；发布前检查 `git status --short --ignored` 和 `git diff --cached`。若凭据已泄露，应移除公开内容并轮换密钥。
- Telegram 通知默认不使用；如需启用，先审查扩展源码并安全配置聊天目标与 Bot Token，**不要将凭据写入仓库**。通知可能把任务输入、回复和提问发往目标聊天，且不保证送达。
- Thinking 翻译与自动会话命名可能向各自配置的模型发送对话内容；使用前确认接收方。
- `/usage` 依赖可能变化的非公开 Codex 用量接口。工具结果过滤不能代替凭据管理或限制扩展的本机权限。

## SoL-Pi

`extensions/sol-pi/` 提供三项机制：

1. **Action Fusion**：`edit`/`write` 可在成功修改后通过 `then_run` 执行验证命令；命令失败不回滚修改，命令本身也不是沙箱。
2. **ObservationPack**：将重复的大型纯文本结果换成可用 `obs_recall` 回读的引用；归档保留在本地会话目录，不自动清理或脱敏。
3. **Online Context Compact（OCC）**：在合适的计划步骤完成后评估压缩并继续任务；摘要可能丢失细节，且会产生额外模型请求与费用，并非省钱保证。OCC 针对 Pi **0.87.1**，在 Worker 中禁用。

来源、配置、兼容限制及离线测试命令见 [SoL-Pi 扩展文档](extensions/sol-pi/README.md)。
