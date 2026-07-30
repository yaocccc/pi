# 中文 Subagents 扩展

一个参考 `pi-subagents` 和 Pi 官方 subagent 示例实现的精简版扩展。子代理运行在独立的 Pi 进程中，继承当前模型与思考强度；默认关闭扩展自动发现、skills 和 prompt templates，只按 agent 白名单显式加载所需工具来源，避免递归编排与工具污染。

## 命令

扩展只注册四个命令：

- `/subagents [任务]`：展示四个 agent，选择后快速启动；未附任务时继续输入任务
- `/subagents-feat [需求]`：`scout → planner → worker → reviewer`
- `/subagents-fix [问题]`：`scout → planner → worker → reviewer`
- `/subagents-review [目标]`：只运行 `reviewer`

`/subagents` 的 agent 选择界面沿用现有扩展的深色背景、半块边界、主题色选中项和紧凑帮助栏。所有命令未提供任务时，都会打开与 `plan.ts` 一致的深色任务编辑器，支持 `@` 文件补全；输入后按 Enter 直接执行，不再二次确认。

## Agent

仅包含四种固定角色：

- `scout`：只读代码侦察与上下文压缩
- `planner`：只读实施计划
- `worker`：唯一可写角色，负责实现与验证
- `reviewer`：只读审查，提供文件路径和行号证据

四个角色均可使用内置只读工具、`ffgrep`、`fffind`、`searchmemory` 和 Web 研究工具；`worker` 额外拥有 `bash`、`edit`、`write`，`reviewer` 仅将 `bash` 用于只读检查。`ask_question`、Plan 状态工具和 `subagent` 不会下放给子进程，避免无 UI 阻塞、污染主计划状态和递归调用。

扩展同时注册 `subagent` 工具，支持：

```json
{ "agent": "reviewer", "task": "审查当前 git diff" }
```

以及顺序链：

```json
{
  "chain": [
    { "agent": "scout", "task": "侦察认证流程" },
    { "agent": "planner", "task": "根据以下结果制定计划：\n{previous}" }
  ]
}
```

`{previous}` 会替换为上一步最终输出。链按顺序执行，同一时间只有一个子代理运行。

## 执行进度

子代理执行期间会按发生顺序统一展示工具调用与模型提供商公开返回的思考内容，两者合计只保留最近 5 条；思考使用深蓝色 `?` 图标，工具使用运行状态图标。默认视图不展示工具输出，展开后仅显示短输出摘要，也不统计或展示总工具调用次数。不支持公开思考的模型不会出现思考动态，被提供商脱敏的思考会显示隐藏提示。工具参数中的常见密钥字段会被脱敏。

## 文件

- `index.ts`：工具、命令与 TUI 渲染
- `runner.ts`：隔离子进程、JSON 事件解析、取消与超时处理
- `workflows.ts`：命令对应的单 agent 与固定链工作流
- `ui.ts`：与现有扩展一致的 agent 选择界面
- `agents.ts`：固定 agent 配置加载
- `agents/*.md`：四种中文角色提示词
- `types/index.ts`：共享类型

## 使用

文件位于全局自动发现目录：

```text
~/.pi/agent/extensions/subagents/
```

在 Pi 中执行 `/reload` 即可加载。不要与另一个同名 `subagent` 工具扩展同时启用，否则 Pi 会提示工具覆盖。

## 安全边界

- 子进程使用显式工具白名单。
- `scout`、`planner` 没有写工具。
- `reviewer` 没有 `edit` / `write`，并被提示保持只读。
- `worker` 是工作流中唯一写入者。
- 子进程使用 `--no-extensions --no-skills --no-prompt-templates` 关闭自动发现，仅通过 `--extension` 加载白名单工具对应来源。
- `ask_question`、`plan_set_todos`、`plan_check_result`、`subagent` 被显式禁止，子进程不能递归调用本扩展。
- 单个子代理默认最多运行 30 分钟，主会话取消会传递给子进程。
