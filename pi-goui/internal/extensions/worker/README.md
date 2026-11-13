# 可选 Worker 适配器

[English](README_EN.md)

本包将第三方 Worker 扩展的结构化记录适配为 Go 查看器的展示状态。它不属于通用 Pi 会话持久化，也不导入或定位 TypeScript 扩展。

- `Transcript` 仅包含版本和记录 UUID。
- `Cursor.Read(ctx, client, descriptor)` 通过 `rpc.Client.CallExtension` 调用 `worker.transcript`，接收完整记录、不透明续读令牌以及 reset/more/partial 标志。
- 路径校验、存储布局、旧布局查找、文件身份检查、增量偏移和限制均归后端所有；本包没有读取文件系统的生产代码。
- `Replay` 与 `EventBatch` 仅重建展示状态，不能恢复或执行 Worker。
- 缺少可选 RPC 桥或能力时，宿主改为显示任务已有结果，不使用本地文件兜底。

Pi 端方法由 `extensions/pi-ui/worker/index.ts` 注册，委托给 `worker:v1.readTranscript`。能力端每次请求最多读取 4 MiB，验证记录头/UUID，仅接受非符号链接的普通文件，并维护有界且会过期的后端游标。文件删除、替换或截断时会安全重置或报错；游标一旦读过主存储位置，就绝不回退到过时的旧布局副本。

测试：`cd pi-goui && go test ./internal/extensions/worker`。Go 测试不依赖本仓库的 TypeScript 路径。
