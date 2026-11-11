---
name: worker-orchestration
description: Use for Worker delegation, Fast/Normal/Deep routing, parallel task splitting, file boundaries, review, rework, and acceptance.
---

# Worker 编排

自主委派边界清晰、可独立验收且值得分担的任务，主 Agent 负责最终 diff、验证和交付。小任务或需连续用户反馈时直接完成；事实不明先调查。Worker 不得继续委派。

## 会话与路由

每项任务的 `sessionMode` 和 `preset` 均必填，不接受省略或 auto。主 Agent 自主选择，不询问用户；用户明确指定时遵循要求。会话模式：

- `fork`：优先选择，继承批次启动时活动分支快照。契约写清目标与边界，不重复粘贴历史。
- `new`：仅在上下文交接很少、简短契约即可完整说明任务时使用；不确定时用 fork。
- 同批可混用；必须显式填写。会话隔离不扩大权限，也不隔离共享工作区。

`preset` 由主 Agent 按难度选，不按文件数机械判断：

- `fast`：明确、局部、易验证的搜索、机械修改、简单测试、常规 Review。
- `normal`：普通功能、Bug、重构和测试；不确定时选此档。
- `deep`：根因不明、并发/一致性、资源生命周期、复杂架构或重要 Review。

## 任务契约

`mode` 决定职责，与 `preset` 分开选择：

- `scout`：只读调查；`review`：只读审查，不顺手修复。
- `implement`：按契约实现；`test`：运行或补充测试，不擅改生产逻辑；`fix`：只修已确认的问题。
- implement/test/fix 均要求非空 `allowedPaths`，即使 test 只打算运行测试。

用可选 `title` 写一句简短任务描述（建议 10–30 字，不机械截断）；UI 优先展示它，缺省或空白时回退 `objective`。完整执行目标仍写入 `objective`；按需提供 `context`、`relevantFiles`、`acceptanceCriteria`、`verificationCommands`、`outputRequirements`，确保目标可验收。Worker 不自动加载 skills 或项目上下文文件；必要规范和命令须在契约中给出或指向具体文件，尤其是 new 会话。

- `cwd` 只能是主工作区内已存在的相对子目录，不可指向工作区外的独立 worktree。
- `allowedPaths`/`forbiddenPaths` 均相对 task.cwd：文件用精确路径（可为新文件），目录后代用 `dir/**`；裸目录和 `dir/` 会被拒绝，不存在的裸路径按精确文件处理。
- `relevantFiles` 仅是读取提示，可为绝对路径或 cwd 外路径，不授予写权限。
- guard 只拦截 edit/write，不是 bash 沙箱；所有工具均须遵守契约，不得借 bash 绕过只读或写入边界。

## 并行与生命周期

每会话仅一个未完成批次；并行任务放在一次 `tasks` 中，不用多个启动或 Promise.all：

- 启动：`worker({tasks:[...]})`，即使单任务也用数组（1–12 项）。
- 续等：`worker({batchId})`。
- 取消：`worker({batchId,cancel:true})`。

启动与控制字段不得混用。第二次启动返回 `activeBatchId`，先续等或取消。启动/续等等待整批完成，不轮询；单项失败不取消兄弟任务。交付前完成或取消所有批次。

只读之间可并行，只读与写入串行；写任务按 cwd、全部 allowedPaths 及解析后的符号链接保守判定冲突，forbiddenPaths 不缩小锁范围。内部别名不能越出规范化写范围；显式别名前缀可用，禁止项同时检查路径和真实目标。共享接口、生成文件或前后依赖应顺序处理，不只看文件名是否不同。

任务保留槽位和路径锁直到清理及结果校验结束，主 Agent 的工具不受锁拦截，不要修改 Worker 锁定路径。期限固定；取消或中断活动调用须等待清理，调用返回后再中断无效。权限/范围变化须取消并回收，再建新任务。

缺少信息、决策或授权时 Worker 返回 `blocked` 并释放资源。主 Agent 处理 blocker，必要时用 `ask_question`，再按新契约派发；不自动重试，不把取消当授权。

## 验收与返工

主 Agent 直接验收小任务和普通任务，不默认追加 Review。需要独立审查时常规用 fast，重要或困难任务用 deep；定向 Fix 仅处理已确认问题，最终仍由主 Agent 验收。

检查实际 diff、原有修改是否保留、验证是否真实执行、逐条验收证据、blocker、越界及未解析输出。blocked/failed 不得当成功。Review finding 须含文件位置、证据、影响、建议与置信度。

并行写任务的 `changed_files` 可能按声明范围归属；`observed_changed_files` 保留完整 Git 观测。共享工作区无法确定范围外改动来源，须检查整批 diff，不能只信 Worker 报告。

实现与排障按需见 [README.md](README.md)。
