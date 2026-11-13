# Worker transcript v1（只读查看器协议）

[English](TRANSCRIPT_EN.md)

Worker CLI/会话隔离保持不变：`new` 仍使用 `--no-session`；`fork` 仍基于捕获的临时分支执行，退出后删除该分支。记录是**独立的只追加事件日志**，不是原生 Pi 会话，绝不能传给 `--session`、恢复执行或由第二个写入 Agent 打开。

## 发现与生命周期

- 根目录：`path.join(path.dirname(agentDir()), "worker-transcripts")`，通常为 `~/.pi/worker-transcripts`（与 `~/.pi/agent` 相邻，而非在其内部）。`agentDir()` 使用现有 Worker 配置解析器：设置了 `PI_CODING_AGENT_DIR` 时使用该值，否则使用 SDK `getAgentDir()`，并解析为路径。新写入与保留策略只使用这个相邻目录；`.retention.lock` 和 `<UUID>.active` 与日志放在一起。通过 `worker.transcript` RPC 向 Go 提供数据的后端读取器先尝试此位置，仅在新根目录或文件不存在（ENOENT）时回退到旧 `agentDir()/worker-transcripts`。新记录不安全、不可读或格式错误均不触发回退。游标一旦打开新位置副本，其被删除后也不会回放旧位置的同 ID 副本。旧文件不会被自动迁移或删除，也不计入新目录容量预算；UUID 描述符保持不变。
- 任务字段：`transcript: { version: 1, id: "<UUID>" }`。它通过 `ChildProgress` 发布，在 `ChildResult`/宿主任务结果中返回，复制到 `WorkerUiTask`，并保留在父会话 `worker-snapshot-v1` 历史中。可选 `incomplete: true, error: "size_limit" | "io_error" | "protocol_error"` 标记已知记录失败；描述符不含文件系统路径。
- **只将校验过的 UUID** 解析为 `<root>/<id>.jsonl`；拒绝未知版本、路径穿越、符号链接文件及符号链接逃逸。不接受任务文本/结果中的任意路径。使用宿主产生的任务描述符，而非模型返回的值。
- 文件采用随机 UUID 名称，以独占方式创建，权限为 `0600`；根目录权限为 `0700`。拒绝已有符号链接根目录。私有权限不等于加密：实际任务消息/工具结果可能包含敏感信息。
- 日志在任务完成/取消及父会话切换后仍保留，但受下文容量预算策略约束。没有描述符的旧任务没有可恢复记录。查看器必须容忍文件缺失，包括只读详情打开期间被清理的已完成旧日志。

## JSONL 分帧与回放

UTF-8 记录以 **LF** 分隔，不使用 Unicode U+2028/U+2029。任务运行期间，读取器必须保留末尾不完整行，不应每帧重新扫描整个日志。原生 JSON stdout 块通过 `StringDecoder` 解码，避免分块破坏多字节字符。

第一条记录：

```json
{"type":"worker_transcript_header","version":1,"id":"12345678-1234-4234-8234-123456789abc","cwd":"/project","entries":[]}
```

对于 **`new` 和 `fork`**，新记录始终写入 `entries: []`。执行中的 fork 仍通过临时原生会话接收捕获分支；继承历史不复制到查看器日志，只记录本任务产生的事件。已有且 `entries` 非空的 v1 日志无需重写仍可读取：通过 `State.LoadEntries` 用头部初始化查看器，保留继承消息边界，并在任务视图中隐藏继承条目。单独从头部设置 cwd，再将后续会话事件行交给 `State.Apply`。

后续行是真实 JSON 模式会话事件，结构与 RPC `State.Apply` 使用的一致：

```json
{"type":"agent_start"}
{"type":"message_start","message":{"role":"user","content":"actual task prompt","timestamp":1}}
{"type":"message_end","message":{"role":"user","content":"actual task prompt","timestamp":1}}
{"type":"message_start","message":{"role":"assistant","content":[],"timestamp":2}}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"Working…"}}
{"type":"tool_execution_start","toolCallId":"call-1","toolName":"read","args":{"path":"file.go"}}
{"type":"tool_execution_end","toolCallId":"call-1","toolName":"read","result":{"content":[{"type":"text","text":"complete output"}]},"isError":false}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Finished"}],"timestamp":3}}
{"type":"agent_end","messages":[]}
{"type":"agent_settled","aborted":false}
{"type":"worker_transcript_end","status":"completed","complete":true}
```

不重复原生 `session` 头部。省略系统消息事件/检查点；完整保留正常用户/助手/工具内容，包括流式增量、工具参数、完整结果及图片，不应用 Activity UI 的摘要规则。保留已知会话生命周期、重试、压缩和自定义条目事件。未知 stdout 诊断记录不是第二套协议；无效 JSON 会将记录标为不完整。

SDK 通常会发出真实用户开始/结束事件。对于未发出这些事件的旧版或失败启动器，写入器会在首个助手/工具事件之前（若无消息到达，则在终止前）插入标准 `message_start` / `message_end` 用户事件，内容为原样传入的任务提示。该兜底记录的是预期输入，并不声称启动失败时执行过它；不会重复正常 SDK 用户事件。不记录环境变量转储或单独提供的 Worker 系统提示。

结束记录：

- `status`：`completed`、`failed`、`cancelled`、`timed_out` 或 `incomplete`。
- `complete: true` 表示记录正常结束，**不表示**任务成功。只有 `status: completed` 表示进程成功结束；任务级验收/结果状态仍可能不同。
- `complete: false` 加 `error` 表示记录提前停止。崩溃后缺失结束记录也表示中断/不完整，绝不表示成功完成。仍在运行的日志通常尚无结束记录。最终刷盘失败时，以最新结束标记及描述符错误标志为准。

## 上限与失败

每个任务日志上限为 64 MiB，并为结束标记预留少量空间。达到上限后停止记录，描述符和结束标记均明确报告 `size_limit`。继承分支大小不再消耗记录预算。不把隐式截断伪装成完整历史。

同步、有界追加保持事件顺序，避免无界的内存 promise/写入队列。文件立即对只读增量查看器可见，正常结束时先 fsync 再关闭。IO 错误不导致 Worker 本身失败或取消。磁盘故障可能无法写入最终标记，因此描述符携带错误，读取器须保守处理缺失/不完整尾部。进程硬终止无法保证最终标记。

## 自动保留策略

- 预算：**10 GiB = 10 × 1024³ 字节**，按本目录中以 UUID 命名的普通 `*.jsonl` 文件总和计算。未超预算时，无论年龄/数量都保留所有日志。仅超预算时才删除符合条件的日志，按**最后修改时间**从旧到新排序（UUID 用于打破同值），直到扫描总量满足预算。没有按天过期或数量限制。
- 仅在记录开始或任务结束时请求检查。日志目录中单个持久 `.retention.lock` 文件（0600）以修改时间记录最近一次扫描尝试。未过期锁使枚举跳过 **24 小时**，包括跨午夜的情况；失败也保留时间戳，避免当天反复扫描。后续生命周期事件可通过独占创建（`wx`）替换过期锁；进程内节流也避免重复检查锁。过期锁替换是尽力而为，不是分布式锁协议：多个进程同时接管可能短暂重叠。清理器在删除前重新检查锁归属及每个日志的身份/活动状态。忽略旧时间分桶标记，不再创建新的此类标记。扫描异步运行，在检查文件/分批删除期间让出执行，读取元数据以及旧版/已完成日志末尾最多 4 KiB，不读取其完整内容。不增加逐事件扫描、周期定时器、守护进程或无界工作队列。
- 写入器在创建日志前独占发布 `<UUID>.active`，并保留到任务执行结束，即使记录更早达到 64 MiB 上限。租约包含宿主进程 PID，在 Linux 上还包含 boot ID 和进程启动 ticks，以防 PID 重用/重启。其他清理器无论日志年龄都会跳过活跃所有者。崩溃租约在所有者退出后可回收；非 Linux 平台对重用中的活跃 PID 保守保护，直到其退出。正常完整记录移除租约；记录提前停止或结束写入失败时，已结束租约允许回收，而无需等待父 Pi 进程退出。扫描会清理孤立的非活跃租约。`.active` 文件是删除保护标记，不是互斥锁：陈旧或格式错误标记不能阻止创建另一任务或读取其详情。子进程失败/取消及同步启动异常都在宿主 `finally` 路径释放或结束标记，即使宿主仍活跃。无法捕获的宿主崩溃可能留下标记；后续计划扫描检测到所有者已退出，但只有目录超预算时才删除对应日志。所有者无法验证或标记格式错误时，可能保守占用磁盘以避免误删；这是残余存储，不是死锁。
- 已有无标记日志仅在尾部包含完整 `worker_transcript_end` 且 `complete: true` 时才有资格清理；旧写入器的大小限制标记不能证明执行已结束。无标记且不完整的旧日志以及格式错误/不确定租约受到保守保护，以免删除旧写入器仍在运行的任务；可能需要在所有 Worker 停止后手动清理。
- 预算是**软限制**，不是同步磁盘配额：活跃/受保护日志可能使总量持续超预算，新写入字节只在下一次符合条件的生命周期检查时核算。若后续没有任务开始/结束，则没有后台清理。已认领扫描崩溃后，会在后续间隔重试。IO 错误跳过清理，不导致任务失败/取消。绝不删除或跟随未知文件、非普通文件和符号链接。

测试：`transcript.test.ts`、`transcript-retention.test.ts` 及本地子进程夹具覆盖生产 stdout 管线和保留策略，无需模型/网络调用或大文件，包括：空 fork 头部且 fork 执行不变、真实任务事件、权限、活跃进程、陈旧崩溃、PID 重用、年龄排序、恰好等于预算时的保留、跨进程节流/并发、符号链接及容错。
