# Pi Coding Agent 配置

[English](README_EN.md)

这是我的个人 [Pi Coding Agent](https://pi.dev) 配置，包含自定义扩展、主题、快捷键和工作流。仓库用于保存可公开、可复用的配置源码；认证信息、模型密钥、会话记录和个人记忆不应提交，并应通过环境变量或被忽略的本地文件提供。

## 功能

- **自定义界面**：启动 Logo、输入框、消息卡片、工作状态和精简 Footer。
- **上下文检查**：`/context` 展示当前上下文窗口的分类占用，并可预览 System Prompt、Tools、Context Files、Skills、用户/Agent 消息和 Tool Call；详情支持空格翻页。
- **Codex 用量**：`/usage` 使用 Pi 当前解析的 OpenAI Codex 认证，显示订阅计划、主/次及附加用量窗口、剩余额度、重置时间和 Credits。
- **结构化提问**：`ask_question` 工具支持单选、多选和自由输入。
- **Plan 工作流**：`/plan` 会生成一份 checklist，确认后按步骤连续执行并进行最终检查。
- **智能提交**：`/commit` 暂存当前全部改动，根据 staged diff 生成简洁的英文 Conventional Commit 信息并提交。
- **分级记忆**：自动维护用户偏好；通过 `memory_search` 检索索引、`memory_get` 按需读取详情；模型可用 `memory_summarize` 请求在本轮结束后沉淀，也可手动执行 `/summarize`。
- **敏感信息过滤**：在工具结果进入模型上下文前过滤常见 API Key、Token、私钥和连接串。
- **Fast 模式**：通过 `/fast` 为支持的 OpenAI Codex GPT-5.6 模型按需启用 priority service tier；内置缺省为关闭，仓库当前 `fast.json` 已启用。
- **Thinking 翻译**：将较短的 Thinking 内容翻译为简体中文，并使用本地持久缓存避免重复翻译。
- **自动会话命名**：`autoname` 在交互式会话的 Agent 完全 settled 后，使用配置模型判断是否保留或更新简洁的中文会话标题。
- **Telegram 通知**：可选地将结构化提问、任务输入和最终回复发送到指定聊天。
- **Worker 编排**：使用独立 Pi 进程并行调查、实现、测试和审查，支持模型分级路由与写入边界校验。
- **扩展包**：集成 `pi-web-access`、`@ff-labs/pi-fff` 和 `context-mode`。
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
│   ├── memory/                        # 分级记忆、工具与 /summarize
│   ├── plan/                          # /plan 工作流
│   ├── telegram/                      # Telegram 任务通知
│   ├── thinking-translation/          # Thinking 中文翻译与缓存
│   ├── ui/                            # TUI 定制
│   ├── usage/                         # /usage Codex 订阅用量
│   ├── worker/                        # 通用 Worker 工具
│   │   └── agents/                    # Worker 执行契约
│   └── herdr-agent-state.ts           # Herdr 管理的扩展状态
├── skills/                            # Agent Skills 与 Worker 编排规则
├── autoname.json                      # 自动会话命名配置
├── fast.json                          # Fast 模式开关
├── memory-settings.json               # Memory 总结与上下文配置
├── thinking-translation-settings.json # Thinking 翻译配置
├── worker-settings.json               # Worker 模型、并发与限制
├── themes/pi.json                     # 自定义主题
├── keybindings.json                   # 快捷键
└── settings.json                      # Pi 全局设置
```

`auth.json`、`models.json`、`models-store.json`、`sessions/`、`memory.md`、`memory-index.md` 和 `memories/` 等本地文件由 `.gitignore` 排除。`.gitignore` 不会保护已经被 Git 跟踪的文件。

## 安装

### 前置要求

- Node.js 22+
- [Pi Coding Agent](https://github.com/earendil-works/pi)
- 可选：[fd](https://github.com/sharkdp/fd)，用于 `/plan` 输入框的文件补全

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

启动 Pi。`settings.json` 中声明的扩展包会由 Pi 管理：

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
| `/plan [任务]` | 生成 checklist，确认后连续执行并最终检查 |
| `/commit` | 暂存全部改动，根据 staged diff 生成 Conventional Commit 信息并提交 |
| `/summarize` | 总结当前任务并沉淀可复用的 indexed memory |
| `/memory_settings` | 交互式配置记忆上限、自动总结、模型、Thinking、通知及上下文来源 |
| `/worker_settings` | 交互式配置 Worker 各档模型、Thinking、并发、自动委派、超时和输出上限 |
| `/fast` | 切换支持模型的 priority service tier；状态保存在 `fast.json` |
| `/thinking_translation` | 切换 Thinking 简体中文翻译 |
| `/autonameall` | 一次性为所有包含用户文本的未命名历史会话批量生成中文名称 |
| `/reload` | 重新加载扩展、Skill、主题和快捷键 |
| `/login` | 配置 Provider 认证 |
| `/model` | 选择模型 |
| `Ctrl+Y` | 打开会话恢复界面 |

`memory_search`、`memory_get`、`memory_summarize`、`ask_question`、`plan_check_result` 和 `worker` 是供 Agent 调用的工具，不需要手动执行。

`/commit` 会先执行 `git add -A`，再将完整的 staged diff 直接交给当前模型生成一行英文 Conventional Commit 信息，最后执行 `git commit`。该命令不启动 Agent 工具循环；执行前请确认工作区中的全部改动都应包含在同一次提交中。

可通过 `/memory_settings` 交互式修改配置，也可直接编辑 `memory-settings.json`。配置项包括记忆数量上限、自动总结、总结模型、思考强度、结果展示方式以及是否包含 Tool Messages 和 Thinking。`summarize.includeToolMessages` 同时控制 Tool Call 与 Tool Result；Memory Tools 的调用和结果始终排除。保存后续总结会使用新配置；切换自动总结时需执行 `/reload` 以同步 `memory_summarize` Tool 的注册状态。`summarize.resultDisplay` 支持 `message`（写入会话）、`popup`（居中弹窗，默认）和 `none`（不通知）；文件或字段缺失时使用内置默认值。`memory_get` 会把最新记忆版本与当前分支最近一次读取结果进行比较：版本相同则提示复用原 Tool Result，内容有更新时才返回完整最新详情；上下文压缩或分支摘要后重新读取完整内容。TUI 总结在后台运行，不阻塞编辑器或后续会话；期间可按 `Esc` 取消模型请求，实际写入开始后为保证索引与详情一致性不再接受取消。弹窗会突出处理结果、记忆标题、关键字段、上下文输入/输出和耗时。

## 可选运行时功能

### Fast 与 Thinking 翻译

`/fast` 修改 `fast.json` 中的开关。启用后只对 `openai-codex` Provider 的 `gpt-5.6-sol`、`gpt-5.6-terra` 和 `gpt-5.6-luna` 请求添加 `service_tier: priority`；内置缺省为关闭，仓库当前 `fast.json` 已启用。

`/thinking_translation` 修改 `thinking-translation-settings.json` 中的开关。当前配置默认开启，使用配置的模型翻译不超过 200 个字符的 Thinking 内容，并将结果缓存到 `~/.pi/thinking-translations/`。

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

Telegram 扩展只负责发送任务通知，不启用 Polling 或远程回复。配置目标 Chat ID 后即可使用：

```bash
export PI_TG_TOKEN='<bot-token>'
export PI_TG_CHAT='<chat-id>'
```

Bot Token 由扩展配置提供。任务结束通知包含项目、会话、本轮用户输入和最终回复；调用 `ask_question` 时还会发送问题和选项，并标记为等待用户回复。发送失败只记录错误，不中断 Agent。

## Worker

`worker` 使用独立、无会话的 Pi 子进程执行任务，支持 Fast、Normal、Deep 三档自动复杂度路由，以及仅由用户显式请求的 Max 执行强度。Deep 是自动路由的最高任务级别。每个档位的完整模型 ID 和 thinking 直接配置在 `worker-settings.json`。Worker 不限制工具列表，并会加载全局扩展，因此后续扩展提供的工具也可直接使用；`PI_WORKER_DEPTH` 仍会阻止 Worker 递归委派。只读任务可并行；批量任务包含写入时会顺序执行。写入任务必须声明允许路径；Worker 子进程会在 `edit` 和 `write` 执行前校验 `allowedPaths`、`forbiddenPaths` 与工作目录边界，并仅使用轻量 Git 状态记录生成变更摘要。可通过 `/worker_settings` 交互式调整各档模型与 Thinking、并发、自动委派、超时和输出上限，也可直接编辑 `worker-settings.json`；保存后续 Worker 任务会立即使用新配置。

Worker 的主要文件为：

- `extensions/worker/index.ts`：工具入口与执行编排；同目录模块负责路由、进程、安全校验和 TUI。
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

启用 Telegram 通知后，项目名、会话名、本轮用户输入、最终回复，以及 `ask_question` 的问题和选项会发送至目标聊天。启用 Thinking 翻译后，符合长度限制的 Thinking 内容会发送给 `thinking-translation-settings.json` 指定的模型，翻译结果会缓存在 `~/.pi/thinking-translations/`。启用自动会话命名后，活动分支中的用户文本、Assistant 文本和现有会话名会发送给 `autoname.json` 指定的模型；执行 `/autonameall` 时，这一范围会扩展到所有项目中符合条件的未命名历史会话。冷却记录仅作为对应会话的 Custom Entry 保存，不进入模型上下文。使用这些功能前请确认数据接收方和模型符合你的隐私要求。

默认忽略的内容包括：

- Provider 登录凭据和模型 API Key
- 会话记录及导出的 JSONL
- 用户画像、记忆索引和项目记忆详情
- Pi 下载或安装的 npm/git 包
- 本地依赖、缓存、日志和临时文件

## 说明

这是面向个人工作流的配置，不是通用 Pi 发行版。扩展拥有本机完整权限，请在使用或修改第三方扩展前审查源码。

`/usage` 依赖 ChatGPT 的非公开用量接口，服务端字段或可用性可能变化；当前仅支持官方 OpenAI Codex Origin。
