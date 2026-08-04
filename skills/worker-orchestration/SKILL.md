---
name: worker-orchestration
description: Use when deciding whether to delegate coding work to the worker tool, selecting Fast, Standard, Deep, or Critical routing, splitting independent subtasks, defining file boundaries and acceptance criteria, planning parallel work, reviewing worker results, or deciding whether rework or an independent review is needed.
---

# Worker 编排

本 Skill 只规定主 Agent 的委派、拆分、路由和验收策略。底层进程、模型发现、并发、取消和路径校验由 `worker` Extension 负责。

## 主 Agent 自己完成

以下工作通常直接完成，不值得委派：

- 纯解释类问题；
- 只需读取少量代码；
- 极少量且位置完全明确的修改；
- 委派成本高于直接完成；
- 高度依赖用户连续反馈；
- 细粒度视觉试调；
- 需要持续和用户实时协商。

## 自主委派

以下工作适合 Worker：

- 边界清晰的实现或明确 Bug 修复；
- 大量代码搜索、调用链调查；
- 测试编写或执行；
- 独立 Review；
- 可以独立验收的子任务；
- 能降低主会话上下文压力的工作；
- 可以安全并行的独立模块。

低风险且边界清晰时自主调用，不先询问用户。主 Agent 始终负责最终 diff、验证和交付。

## 先调查再实施

需求有不确定性，但代码事实能够消除歧义时，先调用 `investigate` Worker。事实未确认前不要直接实施。调查结果应压缩为调用链、根因、证据和建议边界。

## 任务契约

每次委派尽量提供：

```yaml
mode:
objective:
context:
relevantFiles:
allowedPaths:
forbiddenPaths:
acceptanceCriteria:
verificationCommands:
outputRequirements:
```

不要只发送“帮我把这个功能做好”。写入模式必须给出非空 `allowedPaths`；敏感目录应加入 `forbiddenPaths`。验收条件应可通过 diff、编译、测试或明确代码证据判断。

## 并行原则

只有真正独立的任务才并行。

适合并行：

- 不同服务的只读调查；
- 不同模块且写入文件不重叠；
- 独立测试与独立文档；
- 多个互不依赖的分析方向。

不适合并行：

- 修改同一文件；
- 共享接口仍未确定；
- 后一个任务依赖前一个结果；
- 多个 Worker 需要持续协商；
- 共同修改生成文件或锁文件。

写入路径可能重叠时改为顺序执行，或由主 Agent 明确创建独立 Git worktree。不要让 Worker 自行创建 Worker。

## 路由

### Fast — Luna + high

用于目标明确、范围小、失败易检测且成本低的局部工作，例如查找符号、短调用链、CSS/文案、明确类型错误、已有测试、机械修改、简单测试。

### Standard — Terra + high

日常默认。用于普通功能、常规 Bug、多相关文件、API、前后端功能、普通重构、单元或集成测试、中等调用链和普通 Review。不确定 Fast 或 Deep 时选 Standard。

### Deep — Sol + high

用于跨模块/服务/语言、根因不明、并发与异步状态、缓存一致性、重试容错、资源生命周期、数据同步、复杂架构、大范围回归、重要 Review、约束很多或 Terra 已失败的任务。文件多本身不等于 Critical。

### Critical — Sol + xhigh

只用于风险升级：资金、私钥/助记词逻辑、钱包签名、重放、授权与权限边界、合约资金安全、不可恢复数据破坏、核心数据一致性、高风险终审，或用户明确要求 xhigh。复杂但低风险仍使用 Sol + high。

`max` 不参与自动路由。只有用户明确要求 max/最高思考强度/Sol + max 时，任务才可设置 `thinking: max` 和 `userExplicitMax: true`。不支持时接受 `unsupported`，不得伪装或静默改成 xhigh。

## Review 与返工

- 小任务：Worker → 主 Agent 验收。
- 普通任务：Worker → 主 Agent 验收。
- 复杂或重要任务：实现 Worker → 独立 Review Worker → Fix Worker → 主 Agent 验收。
- 高风险任务：实现 Worker → Critical Review Worker → Fix Worker → 独立终审 → 主 Agent 验收。

不要让所有小任务默认走完整 Review 流程。Review finding 必须有文件、位置、证据、影响、建议和置信度；只修复主 Agent 已确认的问题。

## 主 Agent 验收

Worker 声称完成不代表任务完成。主 Agent 至少检查：

1. 实际 Git diff 与修改范围；
2. 用户原有未提交修改是否被保留；
3. 测试和验证命令是否真实执行；
4. 每条验收条件是否有证据；
5. 是否有 blocker、越界写入、模型降级或未解析输出；
6. 是否需要定向返工或独立 Review。

批量结果彼此独立；一个失败不得掩盖其他任务。任何 `blocked`、`failed`、Critical 降级、越界文件或实际模型不匹配都不能直接验收为成功。
