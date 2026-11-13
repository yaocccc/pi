# SoL-Pi

[English](README_EN.md) · [安装总览](../README.md)

提供大工具结果的 ObservationPack 打包/召回，以及基于计划安全边界的后台 Online Context Compact（OCC）。不替换原生文件工具；Action Fusion、Reducer 和上游全局配置加载器未安装。来源：[NVlabs/SoL-Pi](https://github.com/NVlabs/SoL-Pi)（原始提交 `1559b5cb`，选择性移植至 `e1a586af`）；见 [MIT 许可](LICENSE)。

## 安装：两个完整目录

无需 `shared/`、整个仓库、顶层 UI 入口或生成步骤。保持相对目录布局，勿仅复制 index.ts：
~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/sol-pi" "$DEST/ui/sol-pi"
cp -R "$SRC/sol-pi/." "$DEST/sol-pi/"
cp -R "$SRC/pi-ui/sol-pi/." "$DEST/ui/sol-pi/"
pi --no-extensions -e "$DEST/sol-pi/index.ts" -e "$DEST/ui/sol-pi/index.ts"
~~~

也可在 `$DEST/my-ui.ts` 显式注册 UI：
~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/sol-pi/index.ts";
export default function (pi: ExtensionAPI) {
  registerUI(pi);
}
~~~

此时加载能力入口和 my-ui.ts，**不要再加载 UI 入口或整包适配器**。自动发现能力时也不要重复加载能力。UI 不注册/替换能力；缺少 UI 时执行与校验仍归能力，不支持的交互不能视为同意。

RPC 在上述命令添加 `--mode rpc`，默认使用结构化 UI，无需协议环境变量。安装基线为 Node 22.19+ / Pi 1.5.2，SDK、pi-tui、typebox 由 Pi 提供；无需仓库 node_modules。模型、认证与配置仍由用户提供；**安装可用不代表 OCC 已通过该 SDK 的兼容验证**，见下文。

## 工具与数据

- `update_plan` 每次提交完整 `steps`，保留稳定 ID 和顺序；状态为 `pending|in_progress|completed`，最多一个 in_progress。可选 `progress` 含 `files_changed/verification/decisions` 三个字符串数组。先登记未完成步骤，再报告完成；全新 completed 步骤只作历史。完整 schema 见 [tools.ts](extensions/online-context-compact/tools.ts)。
- ObservationPack 只打包 **超过 10 KiB、纯文本、非错误**工具结果；前 **两次 provider 请求**保留全文，之后替换为稳定 ID、首尾和召回引用。短结果、错误、混合媒体、`obs_recall` 结果不打包。存储失败 fail-open，保留原结果。
- `obs_recall` 按 **UTF-8 字节偏移**分页返回原文；从 offset=0 开始，严格跟随 next_offset，不能假设每页固定 4096 字节。不得作为任意文件读取器；受当前会话归档及分页限制约束。
- 归档原文和追加式 ledger 位于会话目录的 `sol-pi/<session-id>/observation-pack/{objects,ledger.jsonl}`；不改写历史。无会话/内存会话使用私有随机临时根，同进程会话共享；退出不自动删除，重启不恢复临时根映射。删除归档后无法召回；归档可能包含敏感工具输出，应按会话数据保护并自行清理。
- 内置适配器隐藏这两个工具的调用/结果（含进度和错误）及计划提醒，不改变模型可见结果；原生 edit/write 展示不变。第三方 UI 可以按普通工具显示。

## 配置与默认语义

OCC 要求持久化 `getSessionFile()` 和有效 compaction 配置。无会话时隐藏/停用 update_plan；`PI_WORKER_DEPTH > 0` 不注册 OCC 工具或处理器，ObservationPack 独立运行。

[settings.ts](extensions/online-context-compact/settings.ts) 默认只读 agent 目录 settings.json，以及**受信任项目**的 .pi/settings.json，使用 Pi 的有效 compaction/retry 设置，不落盘迁移。SDK 的内存 SettingsManager、自定义 agent 目录或运行期覆盖须向 `createOnlineContextCompactExtension` 注入 `resolveSettings`，返回 `{compaction: settingsManager.getCompactionSettings(ctx.model), retry: settingsManager.getRetrySettings()}`；默认解析器不能读取不可见的内存覆盖。

后台模型设置位于 `~/.pi/agent/online-compact-settings.json`（或 `PI_CODING_AGENT_DIR` 下），每个任务开始重新读取并固定：
```json
{"model":"auto","thinking":"auto","service_tier":"auto"}
```

- 缺省均为 auto；model/thinking 分别继承主会话当时设置。显式 model 为 `provider/model-id`（ID 可含 /）；thinking 支持 `off|minimal|low|medium|high|xhigh|max`，仍受模型支持限制。无效配置警告并跳过优化；SDK 可注入 `resolveSummarySettings`。
- service_tier 为 `auto|priority|fast|ultrafast`；auto 不附加字段，其余原样传给后台摘要，不检查/转换 provider 支持，不影响主会话或原生 /compact。不支持时可能失败。
- 工厂 `cacheWriteReadRatio` 默认 **12.5**，允许非负数或 null（无经济价格估计），不是上述 JSON 设置。基于 1,000-token 预计摘要、请求期望、可移除前缀、累计缓存债务和冷却门控；窗口保护可绕过冷却，沿用有效 reserve-token 设置。详见 [economics.ts](extensions/online-context-compact/economics.ts)。
- 每次选中边界增加 **1–2 次模型请求及配置重试**；摘要禁止缓存写入，压缩也使主提示缓存前缀失效。价格、节省和 UI token 差均为估计，不是计费、预算上限或保证。成功 checkpoint usage 不包含所有失败/取消/丢弃的收费工作。
- requestCount/偿债按主 agent `turn_start` 计数（含继续轮次，派发前中止也可能计入），不按网络重试计数；摘要和缓存预热回放不计入。旧计数不追溯校正。同模型且唯一匹配的签名加密 reasoning 可获受 usage 限额约束的前缀信用；不按密文长度估 token，未知 usage 留作保留成本，这仍只是回放成本代理。

## 触发、回放与安全

只有**当前轮成功的 update_plan 将已登记未完成步骤变为完成，且仍有剩余工作**才可授权边界；重复/改号、全新 completed 或全计划完成均不触发，经济门控可延期。嵌套调用依据 SDK 最终 `nestedCalls` 和本轮更新 ID，外层及所有父调用必须成功；不信任输出文本/details。截断、失败、阻塞或取消不授权；父调用失败前计划可能已持久化。

符合条件的 turn_end 冻结快照，后台使用 `ctx.modelRegistry.streamSimple()`、请求时认证及原生 compact 提示/重试；后续 turn_end / agent_before_settle 原子提交摘要与最新计划，保留期间追加消息。仅剩余工作时添加隐藏提醒及 continue:true，不中止主会话、不等待 ctx.compact、不用 sendMessage 重启；空闲结果只等后续边界，不自行写会话或开对话。

- 保留计划/预测历史；完成后新外部任务可重置任务预测，但保留债务与上次压缩时间。输入匹配有界且仅本次运行；旧 SDK 无来源/输入 ID，混合、重复或变形输入保守跳过重置，绕过 input 注入的相同裸消息无法可靠区分。
- 边界 draft **不触发 session_before_compact、session_compact 或原生失败钩子**；依赖钩子否决/观察所有压缩时须关闭 compaction/OCC。手动 /compact 和溢出恢复仍为原生流程。
- 状态与摘要同批提案，提交后从分支重建；之前纯 custom draft 可保留，已有上下文编辑、custom-message 或 compaction draft 则跳过。后续扩展仍可替换/丢弃提案并负责其修改。
- 更正、取消、session/tree/switch/fork/shutdown、模型变化或原生压缩使后台工作取消/失效；提交前核对会话、完整捕获前缀、generation、主模型。普通追加消息/元数据保留，分支摘要、上下文编辑及其他压缩使任务失效；晚到结果不能覆盖新任务。失效 SDK context 不再访问，脱离后台失败不写诊断。
- 空、截断、tool-calling、失败或取消摘要不形成 checkpoint，不破坏现有调度；原始历史不删除，但摘要仍可能丢细节，可查原会话或召回归档。
- OCC 无 before_provider_request 计费钩子；CacheWarmer 回放不追加 OCC 状态、不偿债。原生预热仍可写 usage 并移动 leaf，守卫允许捕获前缀后的这类记录，但不放过冲突上下文变化；用户预热设置不变。
- 通知区分 ready 与 applied；失败只报短原因/耗时，过期任务静默。错误仅用消息，清终端控制符、尽力脱敏凭据/URL，最多 300 字符加省略号；不存原始错误或 OCC 诊断日志。token 差用同一提交前后投影估计，含等待期间消息/继续提醒，不含会话外系统提示、工具定义及后续 OP 变换；增长不报成节省。

实现入口：[extension.ts](extensions/online-context-compact/extension.ts)、[观测打包](extensions/observation-pack/index.ts)、[运行路径](runtime-paths.ts)。

## 第三方 UI：只观察，不控制压缩

通用传输见 [pi-ui](../pi-ui/README.md)。历史 `pi-goui:*` 名称不要求 Go 前端。没有 `sol-pi.compact`、`plan.get/set`、`observation.read` 远程方法；标准 Pi compact 是独立宿主操作。`get_commands` 不列工具/服务，不能用于探测存在；不要发送 /update_plan、/obs_recall 或为探测而发模型提示。未看到识别的状态前维持普通视图。

成功 `tool_execution_end` 的 toolName=update_plan 时读取 result.details（历史成功 toolResult 读取 details）：
```json
{"boundary":true,"completed_step_ids":["inspect"],"progress_recorded":true,"task_status":"active","plan":[{"id":"inspect","goal":"Inspect inputs","status":"completed"},{"id":"apply","goal":"Apply change","status":"in_progress"}]}
```

plan 为完整替换，勿排序或以错误调用参数覆盖已提交计划。boundary 等是转换事实；task_status=active **不表示全部完成时关闭会话**。

初始化/恢复查询历史（响应为最小示例，真实状态还有内部字段）：
```json
{"id":"history-1","type":"get_entries"}
```
```json
{"type":"response","id":"history-1","command":"get_entries","success":true,"data":{"leafId":"entry-plan","entries":[{"id":"entry-plan","parentId":null,"type":"custom","customType":"sol-pi-online-context-state-v1","data":{"version":1,"plan":[{"id":"inspect","goal":"Inspect inputs","status":"completed"},{"id":"apply","goal":"Apply change","status":"in_progress"}]}}]}}
```

从 leafId 沿 parentId 找活动分支，只投影其最新 version=1 状态，不按全树时间戳挑选、不写回。entry_appended 可带同类状态；替换会话/fork/树导航须重建。未知版本、缺失/非法数据降级通用视图，不编造计划，忽略其他内部字段。

RPC UI 加载后通知示例：
```json
{"type":"extension_ui_request","id":"notice-1","method":"notify","notifyType":"info","message":"pi-goui:event:v1:{\"name\":\"context.applied\",\"data\":{\"beforeTokens\":12000,\"afterTokens\":7000,\"generationMs\":2400,\"durationMs\":8000}}"}
```

去掉精确前缀 `pi-goui:event:v1:` 后解析 {name,data}：
- context.ready: {generationMs}，仅生成完成；
- context.applied: {beforeTokens,afterTokens,generationMs,durationMs}，估计可增可减，毫秒生成耗时与总耗时不同；
- context.failed: {durationMs,reason}，保留上下文/计划，可显示警告。

这些事实不需 extension_ui_response，不是持久确认，也无统一严重度；忽略多余字段、未知/畸形消息。缺少通知不证明失败。持久指标在活动分支 `sol-pi-online-context-metrics-v1` custom entry，含 estimated:true 及 applied 指标；不要将历史指标重放为新 toast。

obs_recall 的工具参数及返回 details 示例（原文在 content）：
```json
{"id":"obs_0123456789abcdef01234567","offset":0}
```
```json
{"id":"obs_0123456789abcdef01234567","offset":0,"bytes":4096,"lines":80,"nextOffset":4096,"eof":false}
```

details 使用 nextOffset/eof；文本头为 next_offset/eof，eof=true 时停止。无专用 RPC 点击取片段接口，不要隐式发模型提示或用 UI 任意读宿主路径；ID 未找到、会话改变、偏移非法或归档删除显示普通工具错误。渲染中和未来新增工具先按普通工具回退，勿把内部 details 当授权来源。

## 兼容性与验证

[native-preparation.ts](extensions/online-context-compact/native-preparation.ts) 是 Pi 0.87.1 的 preparation-only 移植，已适配 1.0.2，并曾对 **1.0.4** 跑差分验证；保留 `sol-pi-occ-native-0.87.1` checkpoint 标记以恢复历史文件列表（[许可](extensions/online-context-compact/LICENSE.pi)）。使用活动分支投影，保留工具配对、split turns、旧摘要/累计文件；嵌套文件记录代表尝试，不保证写入成功。生产只用公开导入，测试可用私有比较 API；升级 SDK 须重新验证，不能把本地依赖当受支持验证目标。

历史 runner 固定 **Pi 1.0.4**，曾记录 174/174 及严格类型检查通过，**不是当前 1.5.x 通过声明**；当前宿主完整 .mjs 套件已复现基线失败。`extensions/npm test` 只涵盖 TypeScript 测试，不包含此套件。

~~~sh
# 从 agent 目录运行；要求现有全局 Pi 1.0.4
node extensions/sol-pi/tests/run-global.mjs
node extensions/sol-pi/tests/run-global.mjs --typecheck
~~~

[runner](tests/run-global.mjs) 隔离 HOME、复制到临时目录并链接现有全局依赖；可用 SOL_PI_GLOBAL_ROOT 指定 npm 全局根，类型检查要求已有 TypeScript。不安装依赖、不改全局设置，不调用远程模型；覆盖细节见 [tests/](tests/)，不代表真实计费或完整交互 UI 认证。

卸载时移除能力目录及对应 UI 注册并重载；归档需另行清理，勿误删其他扩展。
