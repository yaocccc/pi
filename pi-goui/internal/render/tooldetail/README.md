# Tool detail renderers

右侧完整详情的独立纯渲染包；主聊天卡片不在此目录。

## 文件与入口

- `render.go`：`Render(item, theme, width, hooks) []string`、静态分发、错误优先、剩余 Details/附件兜底。
- `helpers.go`：分区、字段、完整结构化值、JSON 读取；不含工具专属布局。
- `read.go`：文件内容、起始行、上游截断通知。
- `write.go`：writing/written 状态及完整写入源码。
- `edit.go`：真实 diff 优先，缺省显示 replacement preview。
- `bash.go`：命令、退出状态及输出；`shell`/`powershell` 是同类入口。
- `worker.go`：任务、活动、结果、耗时及精确重复 payload 引用。活动采用状态图标、工具/操作名及完整正文；隐藏活动的 `id`、`at`、内部索引和估算计数，不重复展示 `status/type`，正文及结果中的同名内容不受影响。
- `codemode.go`：脚本、嵌套调用和输出。
- `ask_question.go`：问题、单/多选标记和真实回答。
- `web_search.go`：查询、提供方、结果规模及完整搜索正文；同时承载网页工具的共享元信息展示函数。
- `fetch_content.go`：来源、格式、抓取状态及完整内容。
- `source_check.go`：主张、来源、引用段落和研究元数据，不推断事实结论。
- `get_search_content.go`：存储结果标识、检索条件、分页位置和完整摘录。
- `update_plan.go`：计划步骤状态、提交证据和完成边界，未记录时明确标注预览。
- `web_enable.go`：实际启用、不可用和仍未激活的能力。
- `obs_recall.go`：观察记录分页、后续偏移和原样摘录。
- `generic.go`：未知工具的参数与完整文本/结构化结果，JSON 数字保持原精度。

视觉沿用主聊天工具的行号源码、红绿差异、命令提示、任务活动树和问答选择标记；详情保留完整数据，不继承主卡片的截断。仅展示会话实际保存的内容，上游已截断部分无法恢复。

`render.ToolResultRows` 是 UI 兼容适配层；`ToolResultFrame/Title` 仍负责外框和标题。`Hooks` 仅注入已有的终端基础能力：逐行净化/着色/换行、文本净化、源码高亮行号、diff 行及统计。包不反向依赖父 render，不复制词法高亮器，不依赖 UI 状态。所有 Hooks 必填、纯函数；所有输入均只读。

## 新增工具

1. 新建 `<tool>.go`，实现同包 `(*renderer).<tool>(item, args, details)`，配套 `<tool>_test.go`。
2. 在 `render.go` 增加静态分发；仅新增工具时需要编辑入口。真正的别名可指向相同实现。
3. 用 `line/heading/code/output/value/field` 构建内容。`field` 只在已展示后从当前调用的 details 副本移除字段；修改嵌套 map/slice 前也必须复制。未知字段交给通用兜底。
4. 不套用主视图的 10 行/100 列截断，不读文件系统，不丢真实空行，不把整个多行字符串一次着色；状态及错误必须来自结果数据。
5. 从 `pi-goui` 运行 `go test ./...`。本包测试覆盖分发/别名及兜底；父目录 `tool_inspector*_test.go` 保留真实 ANSI、宽度、完整性和回归测试。

## 并行修改边界

接口稳定后可分别派发：

- 文件类：`read.go`、`write.go`、`edit.go` 及各自测试。
- 执行类：`bash.go`、`codemode.go` 及各自测试。
- 任务/交互类：`worker.go`、`ask_question.go` 及各自测试。
- 网络研究：`web_search.go`、`fetch_content.go`、`source_check.go`、`get_search_content.go` 及测试；网页共享函数在 `web_search.go`，修改其接口需先协调。
- 辅助工具：`update_plan.go`、`web_enable.go`、`obs_recall.go` 及各自测试。
- 兜底：`generic.go` 及其测试，可独立改进。

`render.go`、`helpers.go`、公共测试和父包适配层属于共享边界：修改共享 API 应单独串行完成，不让各任务同时改动。
