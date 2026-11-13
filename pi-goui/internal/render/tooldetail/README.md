# 通用完整工具详情宿主

[English](README_EN.md)

本包负责 Pi 原生工具的完整详情和通用兜底，**不保存第三方扩展专属布局**。第三方实现位于 [`../../extensions/toolrender/detail`](../../extensions/toolrender/detail/)。

## 边界

- `render.go`：入口 `Render(item, theme, width, hooks) []string`、错误优先以及附件/剩余数据兜底。默认路由询问可选适配器，适配器拒绝处理时保留通用渲染。
- `helpers.go`：通用标题、字段和完整结构化值展示。JSON 解码复用 `render/tooldata`，保持 `json.Number` 精度。
- `read.go`、`write.go`、`edit.go`、`bash.go`、`codemode.go`：原生工具视图；源码高亮、diff 与完整原样输出仍使用宿主回调。
- `generic.go`：任意已保存参数和结果，包括未知字段。
- `extensions.go`：仅绑定回调并保留 `WorkerTasks` 兼容入口，不含 Worker/问卷/SoL-Pi/Web 渲染规则。

## 增加视图

第三方视图应添加到 `internal/extensions/toolrender/detail`，在该包的 `Renderer.Render` 中登记。通过 `extensionapi.DetailHost` 复用通用格式化，而非导入本宿主实现。未知工具必须返回 false，保留兜底；Pi 原生工具才在本目录增加视图。

所有回调均按渲染实例隔离、为纯函数且必须提供。输入仅为展示副本；保留未知字段、真实空行、窄屏安全和完整记录输出。不得读取文件、重建缺失的上游历史、将卡片预览限制套用于完整详情，或修改能力状态。

```bash
cd pi-goui
go test ./internal/render/... ./internal/extensions/toolrender/...
```

既有宿主集成测试覆盖实际 ANSI、宽度、路由与兜底；适配器本地测试覆盖回调实例独立性和 Worker 数据投影隔离。
