# Pi Coding Agent 配置

[English](README_EN.md)

这是我的个人 [Pi Coding Agent](https://pi.dev) 配置，包含自定义扩展、主题、快捷键和工作流。仓库用于保存可公开、可复用的配置源码；认证信息、模型密钥、会话记录和个人记忆不应提交，并应通过环境变量或被忽略的本地文件提供。

## 功能

- **自定义界面**：启动 Logo、输入框、消息卡片、工作状态和精简 Footer。
- **上下文检查**：`/context` 展示当前上下文窗口的分类占用，并可预览 System Prompt、Tools、Context Files、Skills、用户/Agent 消息和 Tool Call；详情支持空格翻页。
- **Codex 用量**：`/usage` 使用 Pi 当前解析的 OpenAI Codex 认证，显示订阅计划、主/次及附加用量窗口、剩余额度、重置时间和 Credits。
- **结构化提问**：`ask_question` 支持单题单选/多选与自由输入，也可用 `questions` 一次收集多题，在 TUI 中逐题导航并统一确认。
- **智能提交**：`/commit` 暂存当前全部改动，根据 staged diff 生成简洁的英文 Conventional Commit 信息并提交。
- **Indexed Memory**：通过 `memory_search` 检索索引、`memory_get` 按需读取详情；模型可用 `memory_summarize` 请求本轮结束后沉淀可复用知识，也可手动执行 `/summarize`。
- **敏感信息过滤**：在工具结果进入模型上下文前过滤常见 API Key、Token、私钥和连接串。
- **Fast 模式**：`/fast` 对符合条件的 OpenAI Codex GPT 请求添加 `service_tier: priority`；能否使用该 tier 仍取决于 Provider/模型支持。
- **Thinking 翻译**：将较短的 Thinking 内容翻译为简体中文，并使用本地持久缓存避免重复翻译。
- **自动会话命名**：`autoname` 在交互式会话的 Agent 完全 settled 后，使用配置模型判断是否保留或更新简洁的中文会话标题。
- **Telegram 通知**：可选地将结构化提问、任务输入和最终回复发送到指定聊天。
- **Worker 编排**：独立 Pi 子进程支持分档路由、冲突感知并发、任务进度与用量反馈，以及 `edit`/`write` 路径守卫（非沙箱）。
- **SoL-Pi 优化**：Action Fusion 将文件修改与验证命令融合；ObservationPack 将重复的大结果替换为可回读引用；Online Context Compact 在计划步骤完成时评估压缩收益并自动续跑。
- **Impeccable 设计指令**：`/impeccable` 打开可搜索的指令选择器或直接填入可编辑的 Skill 提示词；本仓库未附带 Impeccable Skill。
- **扩展包**：通过 `settings.json` 声明 `pi-web-access` 和 `@ff-labs/pi-fff`；未声明 `context-mode`。
- **主题与快捷键**：自定义 `pi` 深色主题，并使用 `Ctrl+Y` 打开会话恢复界面。

## 目录结构

```text
.
├── extensions/                        # TypeScript 扩展
│   ├── ask-question/                  # 结构化用户提问
│   ├── autoname/                      # 自动会话命名
│   ├── context/                       # /context 占用与内容预览
│   ├── commit/                        # /commit 智能提交
│   ├── fast/                          # 可选 priority service tier
│   ├── filter-output/                 # 敏感工具结果过滤
│   ├── impeccable/                    # /impeccable 指令选择与提示词插入
│   ├── memory/                        # 分级记忆、工具与 /summarize
│   ├── sol-pi/                        # Action Fusion、ObservationPack、OCC
│   ├── telegram/                      # Telegram 任务通知
│   ├── thinking-translation/          # Thinking 中文翻译与缓存
│   ├── ui/                            # TUI 定制
│   ├── usage/                         # /usage Codex 订阅用量
│   ├── worker/                        # 通用 Worker 工具
│   │   └── agents/                    # Worker 执行契约
│   └── herdr-agent-state.ts           # 可选 Herdr 状态桥接文件（不是 herdr/ 目录）
├── skills/                            # memory、worker-orchestration 两项 Skill
├── autoname.json                      # 自动会话命名配置
├── fast.json                          # Fast 模式开关
├── memory-settings.json               # Memory 总结与结果展示配置
├── thinking-translation-settings.json # Thinking 翻译配置
├── worker-settings.json               # Worker 模型、并发与限制
├── themes/pi.json                     # 自定义主题
├── keybindings.json                   # 快捷键
└── settings.json                      # Pi 全局设置
```

`auth.json`、`models.json`、`models-store.json`、`sessions/`、`memory.md`、`memory-index.md` 和 `memories/` 等本地文件由 `.gitignore` 排除。`.gitignore` 不会保护已经被 Git 跟踪的文件。`extensions/herdr-agent-state.ts` 是受外部 Herdr 管理、仅在相应环境变量齐全时启用的桥接文件；仓库没有 `herdr/` 目录。

## 安装

### 前置要求

- Node.js 22+
- [Pi Coding Agent](https://github.com/earendil-works/pi)；当前 SoL-Pi OCC 已针对 **0.87.1** 验证，升级 Pi 后需重新验证兼容性。

安装 Pi：

```bash
npm install -g --ignore-scripts @earendil-works/pi-coding-agent
```

### 使用此配置

先备份已有配置，再将仓库克隆到 Pi 的全局配置目录：

```bash
mv ~/.pi/agent ~/.pi/agent.backup
git clone <repository-url> ~/.pi/agent
npm ci --prefix ~/.pi/agent/extensions
```

`npm ci --prefix` 在 `extensions/` 内按 `package-lock.json` 安装本地扩展依赖（例如 `node-telegram-bot-api`、`typebox`）；不要把这一步当成 Pi 管理的扩展包安装。启动 Pi 后，`settings.json` 声明的两个 npm 扩展包由 Pi 另行管理：

```bash
pi
```

首次使用时执行 `/login` 配置认证。如需自定义 Provider 或模型，请在本地重新创建 `~/.pi/agent/models.json`，并优先通过环境变量引用密钥；该文件不会被 Git 跟踪。配置方式见 [Custom Models](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/models.md)。

如果需要恢复原有的本地模型配置，可以从备份复制：

```bash
cp ~/.pi/agent.backup/models.json ~/.pi/agent/models.json
```

修改扩展、Skill、主题或快捷键后，可在 Pi 中执行 `/reload`。

## 常用命令

| 命令 | 说明 |
| --- | --- |
| `/context` | 查看上下文分类占用，并预览 System Prompt、Tools、Context Files、Skills 和消息内容 |
| `/usage` | 查看当前 OpenAI Codex 账号的订阅用量、剩余额度和重置时间 |
| `/commit` | 暂存全部改动，根据 staged diff 生成 Conventional Commit 信息并提交 |
| `/summarize` | 总结当前任务并沉淀可复用的 indexed memory |
| `/memory_settings` | 交互式配置记忆上限、自动总结、模型、Thinking 及通知 |
| `/worker_settings` | 交互式配置 Worker 各档模型、Thinking、并发、自动委派、超时和输出上限 |
| `/impeccable`、`/impeccable <指令>` | 在交互式 TUI 选择设计指令，或直接将指定指令的可编辑提示词写入编辑器（不会自动执行） |
| `/fast` | 切换支持模型的 priority service tier；状态保存在 `fast.json` |
| `/thinking_translation` | 切换 Thinking 简体中文翻译 |
| `/autonameall` | 一次性为所有包含用户文本的未命名历史会话批量生成中文名称 |
| `/reload` | 重新加载扩展、Skill、主题和快捷键 |
| `/login` | 配置 Provider 认证 |
| `/model` | 选择模型 |
| `Ctrl+Y` | 打开会话恢复界面 |

`memory_search`、`memory_get`、`memory_summarize`、`ask_question`、`worker`、`obs_recall` 和 `update_plan` 是供 Agent 调用的工具，不需要手动执行。

`/commit` 会先执行 `git add -A`，再将完整的 staged diff 直接交给当前模型生成一行英文 Conventional Commit 信息，最后执行 `git commit`。该命令不启动 Agent 工具循环；执行前请确认工作区中的全部改动都应包含在同一次提交中。

## Indexed Memory

`skills/memory/SKILL.md` 指导 Agent 先搜索索引、必要时按名读取详情；`memory_search` 同一会话内重复查询会提示复用，`memory_get` 比较当前分支最近读取的版本：相同则提示复用，有更新才返回详情。原生压缩或分支摘要后可重新读取；若 OCC 压缩后返回复用提示但旧内容已不可见，应通过明确的读取路径重新获取，而不能依赖该提示。记忆索引和详情分别保存在被忽略的 `memory-index.md` 与 `memories/`，不随仓库分发。

`/summarize` 手动请求总结；启用自动总结时，Agent 也可通过 `memory_summarize` 排队在本轮完全 settled 后总结。总结上下文只取用户文本与 Assistant 最终文本，不含 Thinking、Tool Call 或 Tool Result；模型可判定没有长期价值而不写入。`/memory_settings` 或 `memory-settings.json` 可设置数量上限、自动总结、模型、思考强度和通知方式；保存后的总结使用新配置，切换自动总结工具的注册状态须 `/reload`。`summarize.resultDisplay` 可选 `message`（写入会话；本仓库当前配置）、`popup`（居中弹窗；代码默认）或 `none`（不通知）；配置缺失/无效时使用代码默认。

TUI 总结在后台运行，可继续编辑和对话；进度组件显示阶段、模型、Token 估算/用量与耗时。模型请求期间按 `Esc` 可取消；进入实际写入阶段后不能取消。选择 `popup` 时结果展示处理状态、记忆标题/字段及用量和耗时，可键盘或鼠标滚动；`message` 模式则在会话中展示结果。

## SoL-Pi 上下文与执行优化

`extensions/sol-pi/` 移植自 [NVlabs/SoL-Pi](https://github.com/NVlabs/SoL-Pi)，保留 MIT 许可，由 Pi 自动发现加载。当前启用以下三个机制，**未引入 Evidence-Preserving Reducer**，也不读取上游 `sol-pi.json` 配置。

### Action Fusion

`edit`、`write` 保留原生参数，新增可选 `then_run`，例如 `{"command":"npm test","timeout":30}`。文件修改成功后才执行命令，并在同一次工具调用中返回结果；命令失败不会回滚修改。不传 `then_run` 时沿用原生行为。

融合命令在内部调用 shell，不产生独立的 `bash` 工具调用事件，因此不会自动经过仅针对 `bash` 的工具调用守卫。它不是沙箱，命令的副作用仍需遵守任务范围。

### ObservationPack

大于 **10 KiB** 的非错误纯文本工具结果，前两次模型请求完整发送，之后替换为带首尾摘录的引用。Agent 可通过 `obs_recall` 按字节偏移分页回读原文；短结果、错误结果和混合媒体结果不打包。

此机制仅改变发给模型的上下文投影，不改写原始会话历史。原文与 JSONL 记录保存在对应会话目录的 `sol-pi/<session-id>/observation-pack/` 下，不自动清理；删除归档后将无法回读。归档及摘录不构成脱敏保障。

### Online Context Compact（OCC）

Agent 使用 `update_plan` 维护完整任务计划。只有本轮成功更新计划、刚完成步骤且仍有未完成工作时，才评估是否压缩；不是每次更新都会触发。估算考虑剩余主轮次、上下文增长、缓存重建成本和窗口压力，默认缓存写/读成本比 **12.5** 仅为启发式，不代表实际账单节省。

满足条件时，OCC 使用当前模型生成原生格式摘要，在 Pi **0.87.1** 的轮次边界提交压缩与剩余计划提醒，再自然续跑。取消、失败或无效摘要不会提交压缩，也不会额外启动续跑。一次压缩可能增加一到两次摘要请求及重试费用，且摘要仍可能丢失细节。

OCC 尊重现有 `compaction.enabled`、`keepRecentTokens` 和 `reserveTokens` 设置；无持久会话时禁用，`PI_WORKER_DEPTH > 0` 的 Worker 不注册 OCC 工具或事件。它不通过 `agent_settled` 重启任务，但其边界压缩不会触发原生 `session_before_compact` / `session_compact` 钩子，依赖这些事件的其他扩展需单独评估兼容性。

### 验证与维护

```bash
node extensions/sol-pi/tests/run-global.mjs
node extensions/sol-pi/tests/run-global.mjs --typecheck
```

已通过 **57 项离线测试**与严格类型检查，包括真实 Pi 0.87.1 AgentSession 下的连续压缩、取消、失败、缓存预热隔离和原有两个机制的回归。测试使用临时目录与已安装的全局依赖，不调用远程模型、不修改依赖或锁文件；类型检查需要已有的全局 TypeScript 编译器。

修改后执行 `/reload` 或新开会话。卸载时仅移除 `extensions/sol-pi/` 并重新加载，历史归档可另行清理。来源、SDK 设置注入和详细兼容约束见 [SoL-Pi 扩展说明](extensions/sol-pi/README.md)。

## 可选运行时功能

### Fast 与 Thinking 翻译

`/fast` 修改 `fast.json` 中的开关。启用后，仅当 Provider 为 `openai-codex`、API 为 `openai-codex-responses` 且模型 ID 以 `gpt` 开头时，为请求添加 `service_tier: priority`；不再限于某三个 GPT-5.6 ID。代码默认关闭，仓库当前 `fast.json` 的 `enabled` 为 `true`。是否实际获准使用 priority tier 由服务端决定；Worker 进程也会加载该扩展。

`/thinking_translation` 修改 `thinking-translation-settings.json` 中的开关。当前配置默认开启，使用配置的模型翻译不超过 200 个字符的 Thinking 内容，并将结果缓存到 `~/.pi/thinking-translations/`。

### Impeccable 设计指令

`/impeccable` 在 TUI 中打开可搜索的设计指令选择器；`/impeccable shape`、`/impeccable hooks status` 等可直接选择目录中的精确指令。扩展只把 `/skill:impeccable <指令>` 模板写入主编辑器，供用户补全目标、审查并提交；它本身不执行设计操作。`extensions/impeccable/commands.ts` 列出可用指令。本仓库的 `skills/` 只有 memory 与 worker-orchestration，未提供 Impeccable Skill；实际执行模板前需另行安装可发现的同名 Skill，否则模板不能实现对应设计流程。

### 自动会话命名

`autoname` 仅在有 UI 的会话中运行，并在每次 `agent_settled` 后检查是否需要命名。它只向命名模型发送活动分支中的用户文本、Assistant 文本和现有会话名，不发送 Thinking、Tool Call、Tool Result、Skills 或 Pi System Prompt。手动设置的会话名不会被覆盖；对于未命名或之前由扩展生成的名称，模型必须返回 `keep` 或 `rename`，新标题要求为简洁中文。

当前仓库配置位于 `autoname.json`：

```json
{
  "enabled": true,
  "notify": true,
  "cooldownSeconds": 600,
  "model": "openai-codex/gpt-5.3-codex-spark",
  "reasoning": "minimal"
}
```

`enabled` 控制功能总开关，`notify` 控制名称更新通知。`cooldownSeconds` 以秒为单位，默认 600 秒；设为 `0` 可关闭冷却。每次实际发起命名请求时，扩展都会把时间写入当前会话的 Custom Entry，因此 `/reload` 或恢复会话后仍会遵守当前分支的冷却窗口。`model` 可使用完整的 `provider/model`，也可设为 `auto` 以沿用主会话模型；内置默认值为 `auto`，仓库当前配置则显式使用 `openai-codex/gpt-5.3-codex-spark`。`reasoning` 指定命名请求的思考强度。配置会在每次检查时重新读取，无需 `/reload`；名称实际更新时，通知会显示命名请求的上下文 Token 消耗和耗时。

`/autonameall` 是一次性的历史会话补全命令。它会扫描所有项目的会话，跳过已经命名或不含用户文本的记录，并按顺序使用 `autoname.json` 中的模型和思考强度为其余会话强制生成中文名称。该显式命令不受自动命名开关和冷却窗口限制，也不会改动已有名称；每个实际请求仍会写入冷却记录，避免随后恢复会话时立即再次自动命名。命令结束后会汇报已命名、跳过、失败数量和总耗时。

### Telegram 通知

Telegram 扩展只发送任务通知，不启用 Polling 或远程回复。目前源码读取的目标环境变量是 **`PI_TG_CHAT`**；Bot Token 目前由 `extensions/telegram/index.ts` 中的本地值提供，**`PI_TG_TOKEN` 不是当前实现读取的配置项**。不要提交或公开真实 Token；部署前应审查并改为安全注入，本 README 不包含凭据。缺少 Token 或 Chat ID 时不发送通知。

有 UI 的会话在 `agent_end` 后可发送项目、会话、本轮用户输入与最终文本回复；`ask_question` 的单题 `question`/`options` 会发送等待回复通知（多题 `questions` 不会逐题通知）。发送异常当前被静默忽略，不保证送达，也不支持从 Telegram 回传答案。

## Worker

`worker` 使用独立、无会话的 Pi 子进程（`--mode json --print --no-session --no-skills --no-context-files`）执行 `scout`、`implement`、`test`、`review` 或 `fix` 任务。Fast、Normal、Deep 可自动路由，Max 仅响应用户明确要求；Deep 是自动路由的最高档。各档模型和 Thinking 配在 `worker-settings.json`，所配模型必须可用。Worker 不显式限制工具列表，会加载全局扩展；`PI_WORKER_DEPTH` 阻止递归委派，SoL-Pi OCC 在 Worker 中禁用。

TUI Worker 卡片按任务显示排队/运行/完成状态、档位、轮次、用量、耗时、最近工具与思考摘要及完成结论；工作提示汇总主会话和 Worker 的输入/输出 Token。子进程通过 JSON 事件流回报阶段、工具执行和用量，流式用量包含估算值；UI 摘要经截断/常见敏感字段过滤，**不能当作完整脱敏或安全隔离**。取消、超时、输出超限时会尝试终止子进程；失败不自动重试，主 Agent 仍需核对实际 diff。

批量调度在 `maxConcurrentWorkers` 内运行并保持输入结果顺序：只读任务可相互并行；读写互斥；写任务仅在各自 `task.cwd` 与全部 `allowedPaths` 规范化到主工作区后能证明不重叠时并行。无 glob 的路径（即使当前是目录）只表示精确路径；目录后代必须写成 `dir/**`。相同路径、路径节点的祖先/后代关系、重叠的显式子树或 glob 静态目录前缀、符号链接别名以及根级、悬空符号链接或无法静态解析的 glob 均保守视为冲突；调度器可越过被冲突阻塞的队首任务运行后续独立任务。写入任务必须声明允许路径；Worker 子进程只在 `edit` 和 `write` 工具调用前校验 `allowedPaths`、`forbiddenPaths` 与工作目录边界，**不拦截 `bash`、其他扩展工具或 `then_run` 命令**，不能作为文件系统沙箱。只有与兄弟写任务真实重叠的任务才会把 `changed_files` 限制到其规范化声明范围；串行任务保留完整原始 delta。所有结果另以 `observed_changed_files` 暴露完整快照观察路径；共享 Git worktree 下无法证明范围外路径由哪个任务产生，相关说明会写入 `risks`。可通过 `/worker_settings` 交互式调整各档模型与 Thinking、并发、自动委派、超时和输出上限，也可直接编辑 `worker-settings.json`；保存后续 Worker 任务会立即使用新配置。

Worker 的主要文件为：

- `extensions/worker/index.ts`：工具入口与执行编排；`process.ts`、`ui.ts`、`guard.ts` 等负责子进程事件、TUI 与 `edit`/`write` 守卫。
- `skills/worker-orchestration/SKILL.md`：主 Agent 的拆分、委派、Review 与验收规则。
- `extensions/worker/agents/worker.md`：Worker 的执行纪律和结构化返回格式。
- `worker-settings.json`：模型、thinking、并发、超时和最终输出上限配置。

## 隐私与安全

提交前务必检查暂存区：

```bash
git status --short --ignored
git diff --cached
```

`.gitignore` 只能阻止尚未被 Git 跟踪的文件。若敏感文件曾经提交过，需要先将其从 Git 索引和历史中移除，并立即轮换相关密钥。`filter-output` 只过滤进入模型上下文的工具结果，不能替代凭据管理，也不会阻止其他扩展主动发送数据。

`/context` 的详情只显示在本地 TUI，不会写入会话或额外发送给模型。`/usage` 不直接读取凭据文件，只使用 Pi 解析后的运行时认证，并仅向官方 `https://chatgpt.com` 用量接口发送 Bearer Authorization；自定义或代理 Origin 会被拒绝。执行 `/commit` 时，当前 staged diff 会发送给所选模型用于生成提交信息。

启用 Telegram 通知后，项目名、会话名、本轮用户输入、最终回复及 `ask_question` 单题的问题和选项可能发送至目标聊天；当前 Token 由扩展源码提供而非 `PI_TG_TOKEN`，发布/共享前必须先移除任何真实凭据并轮换已暴露的 Token。启用 Thinking 翻译后，符合长度限制的 Thinking 内容会发送给 `thinking-translation-settings.json` 指定的模型，翻译结果会缓存在 `~/.pi/thinking-translations/`。启用自动会话命名后，活动分支中的用户文本、Assistant 文本和现有会话名会发送给 `autoname.json` 指定的模型；执行 `/autonameall` 时，这一范围会扩展到所有项目中符合条件的未命名历史会话。冷却记录仅作为对应会话的 Custom Entry 保存，不进入模型上下文。使用这些功能前请确认数据接收方和模型符合你的隐私要求。

默认忽略的内容包括：

- Provider 登录凭据和模型 API Key
- 会话记录及导出的 JSONL
- 用户画像、记忆索引和项目记忆详情
- Pi 下载或安装的 npm/git 包
- 本地依赖、缓存、日志和临时文件

## 说明

这是面向个人工作流的配置，不是通用 Pi 发行版。扩展拥有本机完整权限，请在使用或修改第三方扩展前审查源码。

`/usage` 依赖 ChatGPT 的非公开用量接口，服务端字段或可用性可能变化；当前仅支持官方 OpenAI Codex Origin。
