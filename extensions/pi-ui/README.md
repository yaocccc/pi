# Pi UI 适配层

[English](README_EN.md) · [安装](../README.md)

`index.ts` 是可选的**整包装配**入口，不是复制单模块的必需文件。统一承载问卷、Worker、Codex Fast 的原生绘制与交互、共享通知和 SoL-Pi 隐藏卡片策略，不实现工具执行或配置持久化。

- 原生渲染使用 `registerToolRenderer`，不覆盖工具。
- 通过模块内 `capabilities.ts` 的同一事件总线协议查找能力，加载在能力扩展之前或之后均可。
- `/worker_settings`、`/codex-fast` 在此注册；校验与保存调用能力服务。
- 为兼容现有 `pi-goui`，此入口也装配旧 Go RPC 和普通 RPC 交互桥。Go 界面仍由 `pi-goui` 绘制。
- 可选 `pi-ui-rpc` 命令将标准 Pi RPC 请求路由到能力方法：`codex-fast.get/set` 和 `worker.transcript`。桥不读写配置/私有记录；Go 先发现桥再调用，缺少能力时隐藏入口或降级。
- 可改为只加载所需 UI 子目录的 `index.ts`，能力照常独立运行。完全没有适配器时，问卷返回未回答问题及 `unavailable`，不会默认同意；对应的交互桥不可用。

整包放在用户 `extensions/` 下时可自动发现。整包入口与所选模块的入口/手动注册二选一，不要重复加载。更新后重启 Pi / Go 后端（原生 Pi 也可 `/reload`）。不需要旧 `extensions/ui/` 实现，当前仓库也不包含它。

接口与边界见 [`../shared/FRONTENDS.md`](../shared/FRONTENDS.md)。定点验证：

```bash
cd extensions
npx tsc --noEmit
node --experimental-transform-types --test pi-ui/*.test.ts pi-ui/*/*.test.ts shared/*.test.ts
```

`integration.test.ts` 使用真实 SDK 加载器覆盖无适配器、适配器先加载和后加载，`architecture.test.ts` 防止能力源码重新引用 UI。

## 只复制本模块与对应 UI

仅需复制以下**两个完整目录**并保持相对布局，不需要 `shared/`、整个仓库、顶层 `pi-ui/index.ts` 或构建步骤；必要的薄协议辅助代码已随目录附带。

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/worker" "$DEST/ui/worker"
cp -R "$SRC/worker/." "$DEST/worker/"
cp -R "$SRC/pi-ui/worker/." "$DEST/ui/worker/"
~~~

~~~text
my-extensions/
├── worker/index.ts        # 能力及目录内全部文件
└── ui/worker/index.ts     # 可选 UI 及目录内全部文件
~~~

**原生 Pi：直接加载两个入口：**

~~~sh
pi --no-extensions -e "$DEST/worker/index.ts" -e "$DEST/ui/worker/index.ts"
~~~

**也可由使用者自行注册原生 UI**，例如在 `$DEST/my-ui.ts` 中：

~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/worker/index.ts";
export default function (pi: ExtensionAPI) {
  registerUI(pi); // 不注册或替换能力本身
}
~~~

此时加载能力入口与 `my-ui.ts`，不要再加载上述 UI 入口或整包适配器；若能力已自动发现，只补 UI 注册。能力缺少 UI 仍负责执行/校验，不支持的交互应返回不可用，不能默认同意。

RPC 使用同一组入口，增加 `PI_UI_PROTOCOL=1` 与 `--mode rpc`；完整消息格式见下文。Worker/Codex UI 各自附带可选方法桥，组合时只注册一次，其他模块不凭空增加方法。

Pi 1.5.2 提供 SDK、`pi-tui`、`typebox`，这两个目录运行不需要复制本仓库的 `node_modules`。认证、模型与能力配置仍由使用者自行设置，复制源码不代表零配置。保持上述目录名和相对层级，改变布局须自行调整导入。辅助文件同步仅供仓库维护者使用，**使用者无需生成代码**。

同一复制布局与注册方式适用于 `ask-question`、`worker`、`codex-fast`、`sol-pi`、`autoname`、`filter-output`。每个 UI 子目录均有中英文 README。仅 Worker/Codex 需要方法桥；独立副本通过 Pi 事件总线协调，不靠模块全局状态。

## 第三方 UI 传输快速接入

适配器本身没有业务状态；与所需能力一起加载：

~~~sh
PI_UI_PROTOCOL=1 pi --mode rpc --no-extensions \
  -e /path/to/extensions/codex-fast/index.ts \
  -e /path/to/extensions/pi-ui/index.ts
~~~

任意 RPC UI 都可用 `PI_UI_PROTOCOL=1` 选择结构化对话框和事实通知。兼容旧 `PI_GOUI_PROTOCOL=1`；历史 `pi-goui:...:v1` 标题/前缀仅是 wire 标识，不要求使用 Go。未设置标记时，在支持的地方退回标准 select/input/confirm/editor。方法桥本身不依赖这些标记。

1. 发送 `{"id":"discover","type":"get_commands"}`。调用桥前必须发现 `name:"pi-ui-rpc", source:"extension"`，不能把未知斜杠命令交给模型。
2. 发送 prompt，message 为 `/pi-ui-rpc {"id":"op-1","method":"codex-fast.get","params":{}}`。
3. 普通 prompt 响应与操作结果分开处理。实际结果是 `extension_ui_request`、`method:"notify"`，其 message 以 `pi-ui:rpc:v1:` 开头；后缀解析为 `{id,result}` 或 `{id,error:{code,message}}`。匹配 `id:"op-1"`，不是外层通知 ID。
4. 此方法成功结果为 `{"level":"normal","levels":["normal","fast","ultrafast"]}`。缺少桥或能力意味着不可用，不代表允许读取它的私有文件。
5. 请求归属按 Pi 进程/标签隔离。超时或取消只停止本地等待，没有逐操作 cancel 命令，已保存操作不回滚；忽略迟到响应。未知方法/版本安全降级。

目前只有 `codex-fast.get`、`codex-fast.set`（`params:{level}`）、`worker.transcript`（`params:{descriptor,cursor?}`）。每项能力自己的 README 独立定义其数据、权威校验和生命周期，不能根据名字推测其他方法。桥按双重 JSON 编码后的消息计算 32 MiB 预算，超限返回 `response-too-large`，而不是断开 Pi。

读取 Worker 时须改为（或同时）加载 `worker/index.ts`。描述符从 Worker 任务快照原样取得，下面的 UUID 仅为示例；可选 `cursor` 必须是后端上次返回的不透明 token，不是字节偏移：

~~~json
{"id":"read-transport-1","type":"prompt","message":"/pi-ui-rpc {\"id\":\"read-1\",\"method\":\"worker.transcript\",\"params\":{\"descriptor\":{\"version\":1,\"id\":\"11111111-1111-4111-8111-111111111111\"}}}"}
~~~


对话框按对应能力文档的答案格式回复匹配的 `extension_ui_response.id`；取消用 `cancelled:true`。事实通知使用另一前缀 `pi-goui:event:v1:`，后缀为 `{name,data}`，无需回复。适配器不把 TypeScript UI 组件传到 RPC，也不执行能力持久化。
