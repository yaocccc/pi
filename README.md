# Pi Coding Agent 配置

[English](README_EN.md)

这是我的个人 [Pi Coding Agent](https://pi.dev) 配置，包含自定义扩展、主题、快捷键和工作流。仓库只保存可公开、可复用的配置源码；认证信息、模型密钥、会话记录和个人记忆均由 `.gitignore` 排除。

## 功能

- **自定义界面**：启动 Logo、输入框、消息卡片、工作状态和精简 Footer。
- **上下文检查**：`/context` 展示当前上下文窗口的分类占用，并可查看 System Prompt、Tools、Context Files 和 Skills 的具体内容；详情支持空格翻页。
- **Codex 用量**：`/usage` 使用 Pi 当前解析的 OpenAI Codex 认证，显示订阅计划、主/次及附加用量窗口、剩余额度、重置时间和 Credits。
- **结构化提问**：`ask_question` 工具支持单选、多选和自由输入。
- **Plan 工作流**：`/plan` 会生成一份 checklist，确认后按步骤连续执行并进行最终检查。
- **分级记忆**：自动维护用户偏好；通过 `memory_search` 检索索引、`memory_get` 按需读取详情；模型可用 `memory_summarize` 请求在本轮结束后沉淀，也可手动执行 `/summarize`。
- **敏感信息过滤**：在工具结果进入模型上下文前过滤常见 API Key、Token、私钥和连接串。
- **请求优化**：为支持的 OpenAI Codex GPT-5.6 模型设置 priority service tier。
- **Worker 编排**：使用独立 Pi 进程并行调查、实现、测试和审查，支持模型分级路由与写入边界校验。
- **扩展包**：集成 `pi-web-access` 和 `@ff-labs/pi-fff`。
- **主题与快捷键**：自定义 `pi` 深色主题，并使用 `Ctrl+Y` 打开会话恢复界面。

## 目录结构

```text
.
├── extensions/          # TypeScript 扩展
│   ├── ask-question/    # 结构化用户提问
│   ├── context/         # /context 上下文占用与内容预览
│   ├── fast/            # 模型请求优化
│   ├── filter-output/   # 敏感信息过滤
│   ├── memory/          # 分级记忆、记忆工具与 /summarize 命令
│   ├── plan/            # /plan 工作流
│   ├── subagents/       # 中文 Subagents 工作流
│   ├── usage/           # /usage Codex 订阅用量
│   ├── ui/              # TUI 定制
│   └── worker/          # 通用 Worker 工具
│       └── agents/      # Worker 执行契约
├── skills/              # Agent Skills 与 Worker 编排规则
├── memory-settings.json # Memory 容量、总结模型与上下文配置
├── worker-settings.json # Worker 模型与并发配置
├── themes/pi.json       # 自定义主题
├── keybindings.json     # 快捷键
└── settings.json        # Pi 全局设置
```

`auth.json`、`models.json`、`models-store.json`、`sessions/`、`memory.md`、`memory-index.md` 和 `memories/` 等本地文件不会提交到仓库。

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
| `/context` | 查看上下文分类占用，并进入 System Prompt、Tools、Context Files 或 Skills 预览 |
| `/usage` | 查看当前 OpenAI Codex 账号的订阅用量、剩余额度和重置时间 |
| `/plan [任务]` | 生成 checklist，确认后连续执行并最终检查 |
| `/summarize` | 总结当前任务并沉淀可复用的 indexed memory |
| `/memory_settings` | 交互式配置记忆上限、自动总结、模型、Thinking、通知及上下文来源 |
| `/worker_settings` | 交互式配置 Worker 各档模型、Thinking、并发、自动委派、超时和输出上限 |
| `/reload` | 重新加载扩展、Skill、主题和快捷键 |
| `/login` | 配置 Provider 认证 |
| `/model` | 选择模型 |
| `Ctrl+Y` | 打开会话恢复界面 |

`memory_search`、`memory_get`、`memory_summarize`、`ask_question`、`plan_check_result` 和 `worker` 是供 Agent 调用的工具，不需要手动执行。

可通过 `/memory_settings` 交互式修改配置，也可直接编辑 `memory-settings.json`。配置项包括记忆数量上限、自动总结、总结模型、思考强度、结果展示方式以及是否包含 Tool Messages 和 Thinking。`summarize.includeToolMessages` 同时控制 Tool Call 与 Tool Result；Memory Tools 的调用和结果始终排除。保存后续总结会使用新配置；切换自动总结时需执行 `/reload` 以同步 `memory_summarize` Tool 的注册状态。`summarize.resultDisplay` 支持 `message`（写入会话）、`popup`（居中弹窗，默认）和 `none`（不通知）；文件或字段缺失时使用内置默认值。`memory_get` 会把最新记忆版本与当前分支最近一次读取结果进行比较：版本相同则提示复用原 Tool Result，内容有更新时才返回完整最新详情；上下文压缩或分支摘要后重新读取完整内容。TUI 总结在后台运行，不阻塞编辑器或后续会话；期间可按 `Esc` 取消模型请求，实际写入开始后为保证索引与详情一致性不再接受取消。弹窗会突出处理结果、记忆标题、关键字段、上下文输入/输出和耗时。

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

`.gitignore` 只能阻止尚未被 Git 跟踪的文件。若敏感文件曾经提交过，需要先将其从 Git 索引和历史中移除，并立即轮换相关密钥。

`/context` 的详情只显示在本地 TUI，不会写入会话或额外发送给模型。`/usage` 不直接读取凭据文件，只使用 Pi 解析后的运行时认证，并仅向官方 `https://chatgpt.com` 用量接口发送 Bearer Authorization；自定义或代理 Origin 会被拒绝。

默认忽略的内容包括：

- Provider 登录凭据和模型 API Key
- 会话记录及导出的 JSONL
- 用户画像、记忆索引和项目记忆详情
- Pi 下载或安装的 npm/git 包
- 本地依赖、缓存、日志和临时文件

## 说明

这是面向个人工作流的配置，不是通用 Pi 发行版。扩展拥有本机完整权限，请在使用或修改第三方扩展前审查源码。

`/usage` 依赖 ChatGPT 的非公开用量接口，服务端字段或可用性可能变化；当前仅支持官方 OpenAI Codex Origin。
