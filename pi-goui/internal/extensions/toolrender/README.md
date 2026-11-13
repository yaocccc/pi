# 第三方工具视图

[English](README_EN.md)

扩展专属紧凑卡片与完整详情的实际渲染逻辑统一放在这里。这些包不执行工具、不读取扩展文件，也不修改设置。

| 包 | 职责 |
| --- | --- |
| `cards` | Worker 任务卡片及活动、问卷卡片、Codex 标签 |
| `detail` | Worker、问卷、SoL-Pi 和 Web 完整结果视图，以及稳定任务投影 |
| `internal/extensionapi/render.go` | 通用宿主回调与类型契约 |

## 宿主接入

- `cards.New(extensionapi.CardHost)` 为单个渲染实例绑定净化、换行、着色、边框与展开辅助函数。`WorkerLayout` 返回行及通用点击块；`QuestionRows` 返回紧凑问卷行。
- `detail.New(extensionapi.DetailHost).Render(name,item,args,details)` 对未知工具返回 false，交给宿主正常兜底。结果行追加到宿主拥有的本次调用切片；通用完整值格式化仍留在宿主，不在这里复制一套。
- `detail.WorkerTasks(item)` 返回独立展示快照和稳定任务标识，保留未知任务字段，不暴露其他任务的内容。它不是能力或任务控制 API。

可依赖通用 `session`、`config`、`render/frame` 和 `render/tooldata`，但不得反向导入父 `render` 包、`render/tooldetail` 或 `ui`。宿主回调按实例隔离，不存在可变全局渲染注册表；核心兼容入口只做薄转发。

```bash
cd pi-goui
go test ./internal/render/... ./internal/extensions/toolrender/...
```

宿主集成测试保留完整输出、ANSI、宽度、折叠与动画检查；本地测试约束依赖方向、宿主绑定独立性、稳定槽位码以及保护隐私的任务投影。
