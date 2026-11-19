# Pi Go UI 开发指南

[English](AGENTS_EN.md)

适用于本目录，同时遵守上级 `AGENTS.md`。先检查工作区并保留无关修改；未经要求不提交。使用说明见 [README](README.md)，扩展设计与接入规则见[扩展指南](internal/extensions/README.md)。

## 修改约束

- 保持独立分发：不依赖作者目录、个人配置或相邻 TypeScript 源码。显式指定的 Pi 失败时不得静默回退；启动不复制凭据、注入扩展或改写原始设置。
- `internal/` 只含 `core/`、`extensions/` 与 `runtime/`。Pi 默认工具、codemode 和通用交互投影属于 core；第三方实现及专项测试属于 extensions；RPC、进程生命周期、会话文件锁和桌面发现属于 runtime，runtime 不依赖 core 或 extensions。遵守扩展指南中的单入口、接口注入与依赖方向，不添加兼容转发层或测试专用生产 API。
- 每个可写会话同时只有一个锁持有者。独立 TUI 不选主、不共享执行者、不广播实时业务状态；仅在明确写操作前争锁，取得锁后才启动 Pi 并加载最新历史。浏览/聚焦只读磁盘，其他实例占用时无弹窗地显示只读及持有者，定期刷新已落盘记录，保留本地草稿、附件、标签与布局。异步操作捕获原后端、执行周期、标签/查看器、页面及 revision；拒绝过期响应，goroutine 只通过事件循环提交结果。
- 切换/恢复会话保留草稿、排队输入、任务和待处理对话；丢弃需确认。重命名/删除绑定选中目标并短暂获取同一把会话锁，校验路径、格式、文件身份并拒绝符号链接。任务、队列、待答交互、后台 worker 与延迟写入全部结束后关闭 Pi，确认写进程已退出再释放锁；正常交接保持标签可用，不显示为崩溃。退出回收自己拥有的子进程、监听器与等待者。
- 锁和诊断元数据位于用户私有运行目录，不能写进真实 agent 目录，也不能从锁目录反推 agent。检测旧版共享执行者及旧 `<agent>/goui-rpc`，无法确认旧 writer 已退出时安全拒绝，不自动迁移或强删。测试使用独立 HOME。
- OS 锁是归属依据，不能因心跳超时抢占 SIGSTOP 进程，不能 unlink/替换锁 inode。真实 Pi 保留锁的生命周期；父 TUI 异常退出不能让仍存活的 writer 失去保护。残留 Claim 仅在持有原 flock、校验进程启动身份并排除旧 writer/进程组后回收；复用登记锁串行化恢复与争锁，重验 Claim/token，不能误删新持有者记录。启动结果或 writer 归属未知时 fail-closed，禁止降级另起 writer 或自动重发未知操作。空闲后其他实例只恢复可操作，不自动发送旧草稿。浏览器断线不等于它连接的 TUI 退出。
- 恢复历史的磁盘预览仅是本地只读投影，后台加载并校验标签、路径、revision 与后端后应用；发送须等待权威快照。投机预读须有界、去重，不启动后端；缓存命中仍验证文件身份、大小与 mtime，不能只按路径复用。连接期间仅暂存明确的首次发送意图，不覆盖后续草稿，不自动重发未知结果；真实连接失败保留历史并明确提示。
- working 统计只由本次执行的 TUI 累计；同一 TUI 的桌面/Web 镜像自然共用它，独立只读 TUI 不补造本轮实时统计。`agent_end` 不冻结计时，`agent_settled` 才冻结，下一轮重置。runtime 不识别第三方载荷，能力用量从唯一组合入口按每轮独占实例注入。
- `agent_settled` 只表明主运行结束，不单独证明可释放锁；确认 native idle、空队列、无待答交互及扩展活动后才关闭后端。未知活动状态保留占用并说明原因。优先通过 `get_entries` 加载当前完整分支；压缩不替换原始记录，历史刷新失败不能污染既有状态。
- 重型历史解析和批量事件重放在后台独占副本上完成，复制 `State` 结构体不等于隔离其切片、映射与扩展能力；提交校验原查看器与任务代际。统计/命令元数据不得无故使正文缓存或在途布局失效，损坏 reset 保留已有可读内容。Web 回放维持字节与尺寸段上限，并避免每块输出搬移整份历史。
- 渲染只消费展示副本，不读当前磁盘文件补造 diff/历史，不修改能力状态。保留未知字段、原始空行与完整详情，卡片预览限制不能截断保存数据或详情；缺失记录、失败和不完整状态不得伪装成成功。
- 后端文本不可信，复用通用净化与 ANSI/Unicode 安全绘制。布局和命中几何保持一致、窄屏不溢出；图片字节不进入摘要。缓存覆盖内容、宽度、折叠、Provider 身份和查看器版本，拖动/选择期间冻结显示，结束后刷新。
- 用户键位重映射及空数组禁用优先于组件默认键。保留真实终端光标和输入法定位。弹窗及详情优先处理 Esc；无模态拦截时，有活动/待处理提示的中止优先于补全。
- 执行、权限、调度、权威校验与私有存储归后端；前端不按模型名推测业务配置、不访问第三方私有文件。唯一公开的档位启动契约是 agent `codex-fast.json`：每个 TUI 仅读一次，不监听、不写回；恢复子进程档位不得提交共享默认值。通用会话浏览、剪贴板、图片、Git、额度和前端偏好等本地 I/O 不受此禁令影响；额度请求仍需凭据隔离、目标限制和超时。
- 文档只维护三组双语入口：根 README 讲使用，AGENTS 讲修改约束，`internal/extensions/README` 讲接入。中文默认、英文 `_EN.md`，顶部互链；避免新增包级说明、设计副本、逐像素规范或历史验证流水账，类型细节链接源码。

## 验证

在 `pi-goui/` 内运行；迭代优先受影响包的测试，跨模块修改执行：

```sh
go test ./...
go vet ./...
go build ./...
GORACE=atexit_sleep_ms=0 go test -race ./... -timeout=90s
git diff --check
```

修改的 Go 文件运行 `gofmt`。专项协议/格式化/生命周期测试紧邻实现，核心只留真实宿主集成；覆盖空能力、未知协议、失败/取消、迟到响应、跨标签隔离及通用回退，不删断言来迁就实现。

涉及交互时运行 `python3 script/test-ui.py`（临时二进制、隔离 HOME、假 RPC）。可选 `python3 script/test-ui.py --real-only --real-pi "$(command -v pi)"` 通过 [tests/real_rpc_guard.py](tests/real_rpc_guard.py) 仅允许只读查询，禁止模型/工具操作。

夹具自身运行 `PYTHONDONTWRITEBYTECODE=1 python3 tests/test_fake_rpc.py`。性能回归优先使用合成大历史、解析/提交阶段隔离和有界工作量断言，不以小历史或隐藏加载状态代替；可运行 `go test ./internal/core/web -run '^$' -bench '^BenchmarkTerminalHistoryAppend$'` 与 `go test ./internal/extensions/hostui -run '^$' -bench '^BenchmarkWorkerPreparedReplayStages$'`，这些基准不代表端到端时延。

恢复历史的慢连接与发送验收运行 `python3 script/test-resume.py`。Linux 生命周期验收运行 `python3 script/test-session-ownership.py` 与 `python3 script/test-shared-web.py`：临时构建、隔离 HOME/agent/cwd，以真实 PTY/HTTP/WS 和假 RPC 验证按需启动、争锁只读、idle 交接、草稿保留与网页断线保活，不调用模型。

涉及可移植性时，在仓库外临时目录与独立 HOME、`PI_CODING_AGENT_DIR` 下验证二进制 `--check --no-session`；不要复制个人 settings/models/auth。配套 TypeScript 的修改遵守其自身开发及验证指南。

如实报告实际运行的检查、失败与未验证项；单元测试不代表终端、模型或跨平台验收，历史基线失败不得描述为通过。文档修改检查双语一致性、本地链接及删除引用，不必为纯文字变更运行整套应用测试。
