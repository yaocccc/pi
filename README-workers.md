# Pi Worker 系统

安装目录：`/home/chenyc/.pi/agent`（当前未设置 `PI_CODING_AGENT_DIR`，因此使用默认全局目录）。Pi 版本：`0.83.0`。

## 分层职责

| 文件 | 职责 |
|---|---|
| `extensions/worker.ts` | 注册 `worker` 工具；模型发现与路由；独立无会话 Pi 子进程；工具、并发、递归、超时、中断、输出和 Git 路径强制约束 |
| `skills/worker-orchestration/SKILL.md` | 主 Agent 的委派、拆分、并行、路由、Review、返工和验收策略 |
| `agents/worker.md` | Worker 收到单个任务后的执行纪律和结构化返回要求 |
| `worker-routing.json` | 模型、预设、并发、回退、重试和功能开关 |
| `install-workers.mjs` | 本机幂等安装/恢复脚本；只创建缺失文件并补充缺失配置字段 |

Extension 的常驻工具描述只保留必要委派规则；复杂拆分、并行、Review 或验收时，主 Agent 再读取 Skill。

## 自动委派

主 Agent 可对低风险、边界清晰、可独立验收的实现、调查、测试、Review 和定向修复自主调用 Worker，不需要先询问用户。纯解释、少量读取、极小且位置明确的修改、视觉试调或需要持续用户反馈的工作通常由主 Agent 直接完成。

复杂委派、并行规划、模型路由、文件边界、独立 Review 或返工决策会触发 `worker-orchestration` Skill。Worker 完成后，主 Agent 仍必须检查实际 diff、原有修改、测试和验收证据。

## 模式

- `investigate`：严格只读，仅启用 `read,grep,find,ls`。
- `implement`：在 `allowedPaths` 内实现；启用标准读写工具。
- `test`：运行/补充测试；未明确授权时不改生产代码。
- `review`：严格只读；finding 必须包含严重性、文件、位置、证据、影响、建议和置信度。
- `fix`：只修复已确认问题，不做范围外重构。

写入模式必须提供非空 `allowedPaths`。所有模式都禁止提交、推送、发布、部署、真实链上交易、私钥/资金操作和递归 Worker。

## 路由与实际模型

| 档位 | 实际模型 ID | thinking | 用途 |
|---|---|---|---|
| Fast | `openai-codex/gpt-5.6-luna` | `high` | 明确、局部、低风险、易验证 |
| Standard | `openai-codex/gpt-5.6-terra` | `high` | 日常默认开发 |
| Deep | `openai-codex/gpt-5.6-sol` | `high` | 跨模块、复杂状态、架构或重要 Review |
| Critical | `openai-codex/gpt-5.6-sol` | `xhigh` | 资金、签名、权限、不可恢复数据等风险升级 |

三个模型均由 Pi 0.83.0 当前 `openai-codex` 模型目录和 RPC 实测识别，支持：`off, minimal, low, medium, high, xhigh, max`。本系统公开接口只允许 `auto/high/xhigh/max`；旧配置中的 `off/minimal/low/medium` 会提升为 `high` 并记录警告。

当前 `max` 真实可用，但绝不自动使用。只有用户明确要求时，调用任务同时设置：

```json
{
  "thinking": "max",
  "userExplicitMax": true
}
```

若模型目录不再支持 max，工具返回 `unsupported` 和当前最高档位，不会静默改成 xhigh。

## 详细执行 UI

Worker 工具参考 `~/.pi/subagents` 的展示方式提供流式 TUI：

- 工具调用行只显示 `Worker` 或 `Workers ×N`，不重复列出任务摘要；
- 折叠结果不重复显示 `Workers ×N`；按 Worker 分组显示状态、mode、档位、`Turn N`、输入 Token `↑`、输出 Token `↓`、耗时、目标摘要和各自最近 3 条动态；
- 多个 Worker 之间使用内部灰色分隔线；
- Token 在流式阶段优先使用 Provider 报告值，尚未报告时按当前公开输出临时估算；最终以每轮 assistant usage 累计；
- 动态按时间混排执行阶段、工具调用和模型 Provider 公开返回的 thinking；
- 展开视图额外显示完整模型 ID、thinking、attempt、缓存读写 Token、上下文 Token、当前阶段、最近 5 条动态、摘要、修改文件、验证、验收、finding 和风险计数；
- 批量任务显示完成数、并发、累计轮次、累计输入/输出 Token、工具调用数、修改文件数和验证通过数；
- 工具参数和输出只保留压缩摘要，并对 token、secret、API Key、Cookie、Authorization、私钥和助记词字段脱敏；
- 只展示 Provider 公开返回的 thinking，不推断或输出隐藏思考过程；
- UI 最多保留 20 条内部动态；折叠视图每个 Worker 显示最近 3 条，展开视图显示最近 5 条，避免工具输出撑高会话。

在 Pi 中使用 `Ctrl+O` 展开或折叠工具结果。

## Worker Schema

```typescript
type WorkerMode = "investigate" | "implement" | "test" | "review" | "fix";
type WorkerPreset = "auto" | "fast" | "standard" | "deep" | "critical";
type WorkerModel = "auto" | "luna" | "terra" | "sol";
type WorkerThinking = "auto" | "high" | "xhigh" | "max";

interface WorkerTask {
  mode: WorkerMode;
  objective: string;
  preset?: WorkerPreset;
  model?: WorkerModel;
  thinking?: WorkerThinking;
  userExplicitMax?: boolean;
  context?: string;
  relevantFiles?: string[];
  allowedPaths?: string[];
  forbiddenPaths?: string[];
  acceptanceCriteria?: string[];
  verificationCommands?: string[];
  outputRequirements?: string[];
  cwd?: string;
  timeoutMs?: number;
}

interface WorkerToolInput {
  task?: WorkerTask;      // 与 tasks 二选一
  tasks?: WorkerTask[];   // 1..12
  manual?: boolean;       // 关闭自动委派后，手动调用需为 true
}
```

手动示例：

```json
{
  "task": {
    "mode": "investigate",
    "objective": "追踪订单创建调用链并返回文件与行号证据",
    "preset": "fast",
    "relevantFiles": ["src/**"]
  },
  "manual": true
}
```

实际结果的 `execution` 会记录请求/解析档位、模型别名、完整模型 ID、thinking、attempt、升级、回退、实际模型、退出码和取消/超时状态。

## 强制约束

- 每个 Worker 使用独立 `pi --mode json --print --no-session` 子进程，不继承主对话；显式传递 cwd、完整模型 ID、thinking、工具 allowlist 和任务契约。
- 子进程使用参数数组且 `shell: false`；认证沿用当前进程环境但不写入参数、配置或日志。
- 子进程带 `PI_WORKER_DEPTH=1`，并使用 `--no-extensions`；即使误加载，扩展也不会在 Worker 中注册，最大深度固定为 1。
- `investigate/review` 不开放 bash/edit/write；写入模式使用 Prompt 约束加 Git 前后快照双重校验。
- 修改 forbiddenPaths、allowedPaths 外文件或只读模式写入时返回 failed，不自动清理或覆盖用户原有修改。
- 写入任务必须位于 Git 工作区。这样才能区分任务前已有修改和 Worker 新增修改；非 Git 写入安全阻断。
- 默认并发 3；写入路径可能重叠时自动顺序执行。一个批任务失败不会覆盖其他结果。
- AbortSignal、超时、Ctrl+C、Pi reload/退出都会向独立进程组发送 SIGTERM，3 秒后必要时 SIGKILL。
- 单任务最多自动重试/升级一次，且已有写入时不会自动重试。
- 输出按配置截断；只解析最后一个 assistant JSON，不返回隐藏思考或无关完整日志。

## 配置

编辑 `worker-routing.json`：

- 修改模型：更新 `models.<alias>.modelId`，必须使用当前 Provider 的完整 `provider/model-id`。
- 禁用模型：设置 `models.<alias>.enabled` 为 `false`。Fast 同 Provider 回退 Terra；Standard 回退 Sol；Deep 最多回退 Terra并标记降级；Critical 不静默降级。
- 修改并发：设置 `maxConcurrentWorkers`，有效范围 1..16，建议不超过 3。
- 修改超时/输出：`defaultTimeoutMs`、`maxOutputBytes`。
- 关闭自动委派：设置 `automaticDelegationEnabled: false`；之后仅接受带 `manual: true` 的明确手动调用。
- 关闭所有 Worker：将 `extensions/worker.ts` 改名为非 `.ts` 后 `/reload`，或按下方卸载。

配置读取会补充缺失字段，不覆盖已有用户值。损坏的 JSON 会保留原文件并明确报错。模型 ID 失效时只在同一 Provider 中按准确 ID/名称重新发现，不跨 Provider 替代。

## 恢复、安装和卸载

重新补齐缺失文件（不会覆盖已有文件）：

```bash
node ~/.pi/agent/install-workers.mjs
```

恢复路由默认值：先备份并移走 `worker-routing.json`，再在 Pi 中调用一次 Worker；Extension 会根据当前 Provider 的准确模型目录生成缺失配置。不要删除 `auth.json` 或修改认证。

卸载：

```bash
rm ~/.pi/agent/extensions/worker.ts
rm -rf ~/.pi/agent/skills/worker-orchestration
rm ~/.pi/agent/agents/worker.md
rm ~/.pi/agent/worker-routing.json
rm ~/.pi/agent/README-workers.md
rm ~/.pi/agent/install-workers.mjs
```

然后执行 `/reload` 或重启 Pi。不会触碰其他 Extension、Skill、Agent、settings、认证或项目文件。

## 已知限制与排障

- Pi 没有原生“路径沙箱”；写入范围采用 Prompt + Git 前后内容/索引快照校验。检测越界后不会自动回滚，避免破坏用户原有修改，由主 Agent 处理。
- verificationCommands 由 Worker 执行；Extension 会核对结构化结果和工作区，但主 Agent 仍应独立检查关键测试。
- 不使用 Git 的目录只允许只读 Worker。
- Provider/模型不可用、认证失效、命令/依赖缺失、权限问题和外部服务故障会 blocked，不靠升级模型掩盖。
- 工具未出现：确认文件位置后运行 `/reload`，再用 `pi --extension ~/.pi/agent/extensions/worker.ts --no-session -p ...` 检查加载错误。
- 模型问题：运行 `pi --list-models gpt-5.6`，确认三个完整 ID 仍属于同一 Provider；不要修改认证方式。
- Skill 问题：运行 `/skills` 并查找 `worker-orchestration`。
