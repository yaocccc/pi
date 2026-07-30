# Pi Coding Agent 配置

[English](README_EN.md)

这是我的个人 [Pi Coding Agent](https://pi.dev) 配置，包含自定义扩展、主题、快捷键和工作流。仓库只保存可公开、可复用的配置源码；认证信息、模型密钥、会话记录和个人记忆均由 `.gitignore` 排除。

## 功能

- **自定义界面**：启动 Logo、输入框、消息卡片、工作状态和精简 Footer。
- **结构化提问**：`ask_question` 工具支持单选、多选和自由输入。
- **Plan 工作流**：`/plan` 会先生成计划，确认后逐项执行、检查，并进行最终复查。
- **分级记忆**：自动维护用户偏好；通过 `searchmemory` 按需检索索引记忆；使用 `/end` 沉淀长期有效的项目经验。
- **敏感信息过滤**：在工具结果进入模型上下文前过滤常见 API Key、Token、私钥和连接串。
- **请求优化**：为支持的 OpenAI Codex GPT-5.6 模型设置 priority service tier。
- **扩展包**：集成 `pi-web-access` 和 `@ff-labs/pi-fff`。
- **主题与快捷键**：自定义 `pi` 深色主题，并使用 `Ctrl+Y` 打开会话恢复界面。

## 目录结构

```text
.
├── extensions/          # TypeScript 扩展
│   ├── memory/          # 分级记忆、记忆工具与 /end 命令
│   ├── ask-question.ts  # 结构化用户提问
│   ├── fast.ts          # 模型请求优化
│   ├── filter-output.ts # 敏感信息过滤
│   ├── plan.ts          # /plan 工作流
│   └── ui.ts            # TUI 定制
├── skills/              # Agent Skills
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
| `/plan [任务]` | 规划、确认、逐项执行和复查任务 |
| `/end` | 结束当前任务并沉淀可复用的 indexed memory |
| `/reload` | 重新加载扩展、Skill、主题和快捷键 |
| `/login` | 配置 Provider 认证 |
| `/model` | 选择模型 |
| `Ctrl+Y` | 打开会话恢复界面 |

`searchmemory`、`ask_question`、`plan_set_todos` 和 `plan_check_result` 是供 Agent 调用的工具，不需要手动执行。

## 隐私与安全

提交前务必检查暂存区：

```bash
git status --short --ignored
git diff --cached
```

`.gitignore` 只能阻止尚未被 Git 跟踪的文件。若敏感文件曾经提交过，需要先将其从 Git 索引和历史中移除，并立即轮换相关密钥。

默认忽略的内容包括：

- Provider 登录凭据和模型 API Key
- 会话记录及导出的 JSONL
- 用户画像、记忆索引和项目记忆详情
- Pi 下载或安装的 npm/git 包
- 本地依赖、缓存、日志和临时文件

## 说明

这是面向个人工作流的配置，不是通用 Pi 发行版。扩展拥有本机完整权限，请在使用或修改第三方扩展前审查源码。
