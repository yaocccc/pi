# 中文 Subagents 扩展

一个参考 `pi-subagents` 和 Pi 官方 subagent 示例实现的精简版扩展。子代理运行在独立的 Pi 进程中，继承当前模型，但使用各角色独立配置的思考强度；默认关闭扩展自动发现、skills 和 prompt templates，显式重载当前会话中的可用扩展来源，再通过工具黑名单隔离角色权限。

## 命令

扩展只注册四个命令：

- `/subagents [任务]`：展示四个 agent，选择后快速启动；未附任务时继续输入任务
- `/subagents-feat [需求]`：默认 `planner → worker → reviewer`
- `/subagents-feat --scout [需求]`：复杂功能使用 `scout → planner → worker → reviewer`
- `/subagents-fix [问题]`：默认 `planner → worker → reviewer`
- `/subagents-fix --scout [问题]`：根因未知或跨模块问题使用 `scout → planner → worker → reviewer`
- `/subagents-review [目标]`：只运行 `reviewer`

`/subagents` 的 agent 选择界面沿用现有扩展的深色背景、半块边界、主题色选中项和紧凑帮助栏。所有命令未提供任务时，都会打开与 `plan` 扩展一致的深色任务编辑器，支持 `@` 文件补全；输入后按 Enter 直接执行，不再二次确认。

常规 feat/fix 由 planner 先完成制定可靠计划所需的最小代码侦察，再交给 worker 实施。只有任务边界不清、根因未知、跨多个模块或代码库较大时，才使用 `--scout` 增加独立侦察阶段，避免每个任务都重复扫描仓库和多消耗一次模型调用。

## Agent

仅包含四种固定角色：

- `scout`：只读代码侦察与上下文压缩，思考强度 `medium`
- `planner`：完成必要的最小代码侦察并制定只读实施计划，思考强度 `high`
- `worker`：唯一可写角色，负责实现与验证，思考强度 `xhigh`
- `reviewer`：只读审查，提供文件路径和行号证据，思考强度 `medium`

所有角色默认可使用 Pi 内置工具以及主会话中已配置扩展提供的工具。`scout`、`planner`、`reviewer` 仅通过 `excludeTools: edit, write` 排除内置写工具，`worker` 不设置工具黑名单；`reviewer` 仍只将 `bash` 用于只读检查。包括 `ask_question`、Plan 状态工具和 `subagent` 在内的其他已配置工具不再全局排除。

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

`{previous}` 会替换为上一步最终输出，最多内联 100KB；链按顺序执行，同一时间只有一个子代理运行。

## 执行进度

子代理执行期间会按发生顺序统一展示工具调用与模型提供商公开返回的思考内容，两者合计只保留最近 5 条；思考使用深蓝色 `?` 图标，工具使用运行状态图标。默认视图不展示工具输出，展开后仅显示短输出摘要，也不统计或展示总工具调用次数。不支持公开思考的模型不会出现思考动态，被提供商脱敏的思考会显示隐藏提示。工具参数中的常见密钥字段会被脱敏。

## 文件

- `index.ts`：工具、命令与 TUI 渲染
- `runner.ts`：隔离子进程、JSON 事件解析、取消与超时处理
- `workflows.ts`：命令对应的单 agent、默认三阶段与可选侦察工作流
- `ui.ts`：与现有扩展一致的 agent 选择界面
- `agents.ts`：固定 agent 配置加载
- `agents/*.md`：四种角色提示词
- `types/index.ts`：共享类型

## 使用

文件位于全局自动发现目录：

```text
~/.pi/agent/extensions/subagents/
```

运行环境要求 Pi 0.82.1 或更高版本，以支持原生 `--exclude-tools`。在 Pi 中执行 `/reload` 即可加载。不要与另一个同名 `subagent` 工具扩展同时启用，否则 Pi 会提示工具覆盖。

## 安全边界

- 子进程使用 Pi 原生 `--exclude-tools` 工具黑名单；未排除的内置工具和已配置扩展工具默认可用。
- `scout`、`planner`、`reviewer` 排除 `edit` / `write`，`worker` 不排除写工具，是工作流中唯一允许写入的角色。
- 子进程使用 `--no-extensions --no-skills --no-prompt-templates` 关闭自动发现，再通过 `--extension` 显式重载主会话工具对应的扩展来源。
- 如果某个扩展来源只提供被排除的工具，则不加载该来源；如果同一来源同时提供允许和被排除的工具，则加载来源并由 `--exclude-tools` 禁用具体工具。
- SDK 注入且无法通过路径重载的虚拟工具不会进入子进程。
- 黑名单只禁用工具名，不构成文件系统沙箱；`bash` 或自定义工具仍可能写文件，非 `worker` 角色还会通过系统提示词约束为只读。
- 单个子代理默认最多运行 30 分钟，主会话取消会传递给子进程。
