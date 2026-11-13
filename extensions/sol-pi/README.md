# SoL-Pi 中文概览

[English / 完整说明](README_EN.md) · [安装](../README.md)

## 只复制本模块与对应 UI

仅需复制以下**两个完整目录**并保持相对布局，不需要 `shared/`、整个仓库、顶层 `pi-ui/index.ts` 或构建步骤；必要的薄协议辅助代码已随目录附带。

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/sol-pi" "$DEST/ui/sol-pi"
cp -R "$SRC/sol-pi/." "$DEST/sol-pi/"
cp -R "$SRC/pi-ui/sol-pi/." "$DEST/ui/sol-pi/"
~~~

~~~text
my-extensions/
├── sol-pi/index.ts        # 能力及目录内全部文件
└── ui/sol-pi/index.ts     # 可选 UI 及目录内全部文件
~~~

**原生 Pi：直接加载两个入口：**

~~~sh
pi --no-extensions -e "$DEST/sol-pi/index.ts" -e "$DEST/ui/sol-pi/index.ts"
~~~

**也可由使用者自行注册原生 UI**，例如在 `$DEST/my-ui.ts` 中：

~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/sol-pi/index.ts";
export default function (pi: ExtensionAPI) {
  registerUI(pi); // 不注册或替换能力本身
}
~~~

此时加载能力入口与 `my-ui.ts`，不要再加载上述 UI 入口或整包适配器；若能力已自动发现，只补 UI 注册。能力缺少 UI 仍负责执行/校验，不支持的交互应返回不可用，不能默认同意。

RPC 使用同一组入口，增加 `PI_UI_PROTOCOL=1` 与 `--mode rpc`；完整消息格式见下文。Worker/Codex UI 各自附带可选方法桥，组合时只注册一次，其他模块不凭空增加方法。

Pi 1.5.2 提供 SDK、`pi-tui`、`typebox`，这两个目录运行不需要复制本仓库的 `node_modules`。认证、模型与能力配置仍由使用者自行设置，复制源码不代表零配置。保持上述目录名和相对层级，改变布局须自行调整导入。辅助文件同步仅供仓库维护者使用，**使用者无需生成代码**。

SoL-Pi 提供大工具结果归档和 Online Context Compact 在线上下文压缩。只加载 `sol-pi/index.ts`，不要重复加载内部入口。它不依赖 Go 前端；原生隐藏工具卡片与通知由可选 pi-ui 提供。

## 使用前

- 配置自己的 Pi 模型与认证；不要复制作者 settings、auth 或模型 ID。
- 在线压缩会调用模型，成本和触发行为由模型、缓存经济参数及配置共同决定。
- 归档记录可能含敏感工具输出，保存在本地，不会自动删除；请自行制定保留策略。
- `obs_recall` 用于召回已归档结果，`update_plan` 维护计划；能力返回结构化结果，不负责终端渲染。
- 配置键、成本与安全限制见下文；不要把摘要估计值当作账单或保证节省。

## 工作机制与成本

ObservationPack 对**大于 10 KiB** 的纯文本非错误工具结果，先保留前**两次 provider 请求**的完整正文，再投影为稳定 ID、开头和结尾。`obs_recall` 使用 `{"id":"obs_…","offset":0}` 按 `next_offset` 分页召回准确字节内容；短结果、错误、混合媒体及召回自身结果不压缩。归档与追加式 ledger 位于会话目录的 `sol-pi/<session-id>/observation-pack/{objects,ledger.jsonl}`；内存/无会话模式使用进程内会话共享的随机私有临时目录，退出后不删除，但重启不会恢复映射。不重写历史，删除归档将失去召回能力。

`update_plan` 替换完整计划，步骤含 `id`、`goal` 及 `pending/in_progress/completed`，可选 progress 含 `files_changed/verification/decisions` 数组。只有本轮成功完成**此前登记的未完成步骤**且仍有剩余工作，才可能成为 OCC 安全边界；新建的 completed 只是历史，重复/重编号不会重置预测，全部完成不触发。经济条件可延后压缩。

合法 `turn_end` 冻结快照，用任务时认证及原生 compact 提示/重试通过 `ctx.modelRegistry.streamSimple()` 后台生成，不等待它完成。后续 `turn_end/agent_before_settle` 原子提交摘要和最新计划，保留后台期间追加的消息；仅在剩余工作存在时加隐藏提醒及 `continue:true`，不 abort 主会话、不 await ctx.compact、不借 sendMessage 重启。

嵌套 update_plan（包括 codemode）必须匹配本轮自身更新 ID 与 SDK 最终 nestedCalls 记录，外层及所有中间调用成功才授权。缺失/截断记录、失败/blocked、取消轮次均不授权，父调用失败前已保存的计划仍可能存在。完成计划后的新外部任务重置任务预测，但保留累计缓存债务和最后压缩时间；排队输入采用本轮有界匹配，来源混杂、重复或变形时保守不重置。Pi 1.0.2 不提供来源/输入 ID，直接由 SDK 注入的同文用户消息无法可靠区分。

- OCC 要求持久化 `getSessionFile()`；无会话时隐藏/停用 update_plan，`PI_WORKER_DEPTH > 0` 不注册 OCC 工具/处理器，ObservationPack 不受此限制。
- SDK 的内存 SettingsManager、自定义目录或运行时覆盖需向 factory 注入 `resolveSettings: ctx => ({ compaction: settingsManager.getCompactionSettings(ctx.model), retry: settingsManager.getRetrySettings() })`；默认 resolver 无法读取宿主私有内存设置。
- `cacheWriteReadRatio` 默认 **12.5**，接受非负数或 null（不估算价格），是启发式而非供应商实测价格或节省保证。估计 1,000-token 摘要、剩余请求预测、首次倍率、后续裕量和累计债务共同决策；原生可移除前缀含 split turn/被替换摘要，并按可见投影上限约束。重压缩有冷却，窗口保护可绕过它但沿用有效 reserve tokens；决策与债务使用估计摘要而非实际账单。
- 总上下文以原生 usage 为锚点。Responses API 中唯一匹配、未改变、同模型的加密推理可按该 assistant 消息 usage.reasoning 给可移除前缀记账，每消息一次并扣除已计可见 thinking；不对密文长度分词。移除/保留推理共享「当前 usage 减可见投影」预算；不匹配、无签名、跨模型或无效/失效 usage 不记信用，未归因用量留在保留成本中。这是回放成本代理，不是精确输入计费或保证界限；不重跑 OP 状态 hook，不改变原生摘要输入。
- 每次选中边界需**一或两次额外模型请求**及配置的重试，split turn 与前缀摘要分别校验。摘要禁用缓存写入（cacheRetention:none），压缩也会使主提示缓存前缀失效。
- 持久化 requestCount 与偿债/请求预测按主 Agent 的 turn_start 计数（含 OCC 继续轮），不是传输尝试；轮内重试不额外计，派发前取消仍可能已计。上下文增长在轮起始采样，摘要请求和 CacheWarmer 重放不计入；旧计数不追溯修正。
- 成功摘要 usage 写入压缩记录；失败、取消、重试或随后丢弃仍可能计费，成功记录不能代表全部成本。这不是账本或预算上限。

## 兼容与安全边界

原生准备函数不公开导出。`native-preparation.ts` 是 Pi 0.87.1 的**仅准备逻辑移植**，已针对 1.0.2 调整、在 1.0.4 做差分验证，保留其 [MIT 许可](extensions/online-context-compact/LICENSE.pi)。使用公开投影 API 与真实活动分支，保留 context edits、split turns、工具配对、旧摘要及累计文件列表（含嵌套文件调用）。稳定的 `sol-pi-occ-native-0.87.1` 标记保留旧 fromHook 文件列表；生产只导入公开 API，私有对照仅用于测试。升级 Pi 必须重新验证，不以仓库本地依赖版本代替历史固定目标。

边界 draft 提交**不触发 session_before_compact/session_compact 或原生压缩失败 hook**。依赖这些 hook 拦截所有压缩的扩展无法拦截 OCC；需要时关闭压缩/OCC。手动压缩仍用原生 hook 并协调去重。状态在同一 draft batch 提议，不预先生效，后续从已提交分支重建；之前纯 custom draft 保留，之前改变上下文/custom-message/compaction 的 draft 会让 OCC 跳过，后续扩展可替换/删除 batch 并对自己改动负责。

中止、延后、空、截断或调用工具的摘要不会成为检查点。纠正输入、取消边界、会话/树/切换/fork/关闭、模型变化、原生压缩均会使任务失效；提交前检查会话、完整快照前缀、generation 和主模型。普通追加消息/元数据保留，冲突 context edits、其他压缩或分支摘要使任务失效，迟到结果不能覆盖新任务。异步回调先检查取消/generation，reload/替换/SDK释放后不再访问失效 ctx，不写诊断日志。OCC 不删除原历史，但摘要仍会损失细节，可查看原会话或用 obs_recall 召回归档。

OCC 不注册 before_provider_request 计量 hook：Pi 1.0.2 CacheWarmer 复用主请求 onPayload，重放不追加 OCC 状态、不偿债、不通过 OCC 改变 leaf。原生预热自身的 usage 记录可与普通追加消息/元数据一起保留，冲突上下文修改仍使摘要失效；用户预热设置及原生 usage 计量不变。

历史 1.0.2 审核确认投影/空历史、usage有效性、切点/恢复后缀、重复压缩选择不变；文件追踪适配 nestedCalls，允许记录失败/未结束调用中已知路径，但这只是尝试记录，不证明写入成功。仅本模块稳定标记补充 fromHook 文件列表，未知 hook 排除。公开 compact 参数保持兼容，认证、重试、取消、无缓存、原子 draft 与原提示继续有历史测试，嵌套授权按最终记录及成功祖先判断。

上游 `e1a586af0ad8956f42ae5b26bba20e48fbf30e00` 是选择性移植来源，并非整树复制：包含压缩后缓存成本、实际可移除前缀、累计债务、最终计划/progress 语义与临时归档修复，另加本地投影定价及排队任务处理。不引入上游 abort/restart 继续方式、宣传 UI 或已移除的 AF。对应原生 UI 适配只隐藏 obs_recall/update_plan 的调用/结果（含进度/错误）及继续提醒，不改模型可见结果、原生 edit/write 或执行行为；只加载能力时不会覆盖 renderer。

## 后台压缩配置

`~/.pi/agent/online-compact-settings.json`（也可位于自定义 `PI_CODING_AGENT_DIR`）：

```json
{
  "model": "auto",
  "thinking": "auto",
  "service_tier": "auto"
}
```

`auto` 分别沿用任务启动时主会话的模型和思考强度。指定模型使用 `provider/model-id`（模型 ID 可包含 `/`）；`thinking` 可设为 `off`、`minimal`、`low`、`medium`、`high`、`xhigh`、`max`，具体取决于模型。每个任务重新读取并固定配置，不改主会话；缺省为 `auto`，无效值跳过本次优化并提示。SDK 宿主可注入 `resolveSummarySettings` 指定自己的来源。

`service_tier` 支持 `auto`（默认）、`priority`、`fast`、`ultrafast`。`auto` 不附加该字段；其他值原样加入后台摘要 payload，不进行值映射或模型/供应商检查。不影响主会话或原生 `/compact`；服务端不支持该参数时本次后台摘要可能失败。

不显示原生压缩进度、不阻塞对话，也不接管 `/compact` 或 overflow 恢复。通知区分「摘要就绪」与「实际提交」，分别报告生成时间、token 变化和含安全边界等待的总耗时。空闲时完成的摘要等待下一安全边界，不自行开启对话或写会话。失效/取消的任务不报失败；实际失败显示耗时与简短原因，不报告成功。

失败原因仅取异常消息，排除堆栈和序列化请求，移除终端控制字符、尽力脱敏常见凭证及 URL，最多保留 300 字符另加省略号，不保存原始错误日志。没有 OCC 诊断日志或仅为日志运行的逐轮经济性评估；经济判断仍在合法边界执行，加密推理计量修复保留。

token 变化使用同一估算法比较实际提交边界前后的会话投影，包含后台期间新增消息与本次继续提醒，排除提交之后的新消息。它不是供应商实测/计费用量，不含会话外系统提示、工具定义或后续 OP 转换，可能不同于界面计数；摘要变大时如实报告增加。

## 验证限制

历史隔离验证器 `tests/run-global.mjs` 固定全局 Pi **1.0.4**，不安装依赖。当前扩展包使用 1.5.x SDK；SoL-Pi 完整 .mjs 套件在当前宿主下存在已复现的基线失败，不能宣称全套通过。

扩展包 `npm test` 只运行 TypeScript 测试，不包含该 .mjs 套件。在 agent 目录运行历史独立验证器：

```sh
node extensions/sol-pi/tests/run-global.mjs
node extensions/sol-pi/tests/run-global.mjs --typecheck
```

验证器隔离 HOME，只复制本扩展到临时目录并链接**已有**全局依赖，不改 node_modules、manifest、lockfile，不安装编译器。`SOL_PI_GLOBAL_ROOT` 可覆盖 npm root -g；类型检查使用现有全局 TypeScript 或 ts-node 附带编译器。

历史固定 **Pi 1.0.4** 的记录为 10 文件 **174/174 通过**及严格类型检查通过，**不是当前 1.5.x 的结果**。覆盖 OP 字节准确 UTF-8 召回/存储 fail-open、OCC 非阻塞生成/安全提交、计划与经济边界、模型设置、原生准备差分、nestedCalls 祖先、排队输入、取消/重试和生命周期；真实 AgentSession 配合认证伪 provider，不调用远程模型。三项 CacheWarmer 回归通过仅测试可见的私有 refresh 验证 idle/streaming 重放与债务隔离、摘要提交/主轮计数、预热 usage 后的冲突 context edits 拒绝。

这些检查不使用真实凭据、不修改全局设置，也不证明真实计费或完整交互 UI。请区分历史通过记录、当前基线失败与未验证交互。

## 第三方 UI 快速接入（独立指南）

### 加载并观察，不在 UI 实现压缩
~~~sh
PI_UI_PROTOCOL=1 pi --mode rpc --no-extensions \
  -e /path/to/my-extensions/sol-pi/index.ts \
  -e /path/to/my-extensions/ui/sol-pi/index.ts
~~~
使用自己的 Pi 认证与设置。保留两个复制目录内的全部文件，不要只复制 index.ts。基线为 Node 22.19+ 与当前验证过的 Pi 1.5.2。PI_UI_PROTOCOL=1 启用结构化扩展 UI；兼容历史 PI_GOUI_PROTOCOL=1 别名及 pi-goui:* 消息名称，不需要 Go 前端。旧版适配器若尚不识别 PI_UI_PROTOCOL，可把该环境变量替换为 PI_GOUI_PROTOCOL=1，以下消息格式完全相同。

Online Context Compact 需要启用配置和持久化主会话（不是 Worker），模型/认证由用户配置。ObservationPack 是独立归档行为，无持久化会话时可能使用临时存储。UI 是**只读观察者**：没有 sol-pi.compact、plan.get/set、observation.read 远程扩展方法。标准 Pi compact 是宿主的另一项操作，不是 OCC 控制接口。

get_commands 是标准发现请求（{"id":"commands-1","type":"get_commands"}），但只列命令，不列工具/服务。不要据此判断 SoL-Pi 是否存在，也不要发送 /update_plan 或 /obs_recall：它们是工具名，不是扩展命令。在有效计划/结果/事实到来之前保留普通 Pi 视图，不显示可选计划/进度组件。不要为了探测能力而调用模型。

### 实时与历史计划
成功的 toolName="update_plan" 的 tool_execution_end 从 result.details 读取计划（历史成功 toolResult 则读取 details），例如：
~~~json
{
  "boundary": true,
  "completed_step_ids": [
    "inspect"
  ],
  "progress_recorded": true,
  "task_status": "active",
  "plan": [
    {
      "id": "inspect",
      "goal": "Inspect inputs",
      "status": "completed"
    },
    {
      "id": "apply",
      "goal": "Apply change",
      "status": "in_progress"
    }
  ]
}
~~~

plan 是完整替换，不是增量 patch。每步有 id、goal、status（pending/in_progress/completed）；保留 ID 与给定顺序，不自行排序。boundary/completed_step_ids/progress_recorded 说明已记录的转换；当前 task_status 为 active，**不能**把所有步骤 completed 当成关闭 Agent 会话的信号。工具错误不能用 proposed arguments 覆盖已提交计划。

初次打开/恢复时发送 {"id":"history-1","type":"get_entries"}。以下为最小示意响应，真实状态记录还包含其他字段：
~~~json
{
  "type": "response",
  "id": "history-1",
  "command": "get_entries",
  "success": true,
  "data": {
    "leafId": "entry-plan",
    "entries": [
      {
        "id": "entry-plan",
        "parentId": null,
        "type": "custom",
        "customType": "sol-pi-online-context-state-v1",
        "data": {
          "version": 1,
          "plan": [
            {
              "id": "inspect",
              "goal": "Inspect inputs",
              "status": "completed"
            },
            {
              "id": "apply",
              "goal": "Apply change",
              "status": "in_progress"
            }
          ]
        }
      }
    ]
  }
}
~~~

从 leafId 沿 parentId 识别活动分支，只投影该分支最新的已识别 version=1 custom state，不能取整棵树中时间最近的状态。UI 不应修改或写回记录。实时 entry_appended 也可带相同 custom state；会话替换、fork、树导航后需重建显示。未知版本或缺失/无效状态使用通用回退，不伪造计划。其他状态字段属内部实现，UI 投影应忽略。

### 进度事实及准确传输格式
启用 pi-ui 与结构化标记后，标准 notify 消息携带事实：
~~~json
{
  "type": "extension_ui_request",
  "id": "notice-1",
  "method": "notify",
  "notifyType": "info",
  "message": "pi-goui:event:v1:{\"name\":\"context.applied\",\"data\":{\"beforeTokens\":12000,\"afterTokens\":7000,\"generationMs\":2400,\"durationMs\":8000}}"
}
~~~

剥离精确 pi-goui:event:v1: 前缀后解析 {name,data}：
- context.ready：{generationMs}，摘要生成完成，**不等于已经应用**。
- context.applied：{beforeTokens,afterTokens,generationMs,durationMs}；token 估算可能增大或减少，生成耗时与总耗时不同，单位都是毫秒。
- context.failed：{durationMs,reason}；保留现有上下文/计划，可显示警告，不假定发生破坏性失败。

忽略新增字段。事实不是持久化确认，不需要回复 extension_ui_response，也可以完全不展示；没有通用严重级别，文案和视觉策略由 UI 决定。

~~~js
// event is one JSON object decoded from Pi stdout (JSONL), not a whole chunk.
function contextFact(event) {
  if (event.type !== "extension_ui_request" || event.method !== "notify") return;
  const prefix = "pi-goui:event:v1:";
  if (typeof event.message !== "string" || !event.message.startsWith(prefix)) return;
  try {
    const fact = JSON.parse(event.message.slice(prefix.length));
    if (["context.ready", "context.applied", "context.failed"].includes(fact.name)) {
      return fact; // Render in your own UI; no extension_ui_response is sent.
    }
  } catch {} // Ignore malformed/unknown extension payloads.
}
~~~

已保存的应用指标位于活动分支 customType="sol-pi-online-context-metrics-v1" 的 custom entry，data 含 estimated:true 及上述应用指标。不要把每条历史事实都重新弹成通知；缺少事实也不代表失败，适配器或通知传输可能没有加载。

### ObservationPack 展示
观察普通 obs_recall 工具输出，其 details 为：
~~~json
{
  "id": "ID_FROM_TOOL_RESULT",
  "offset": 0,
  "bytes": 512,
  "lines": 12,
  "nextOffset": 512,
  "eof": false
}
~~~

id 不透明，offset/nextOffset/bytes 按字节计，lines 为行数，eof 标识归档结束。使用 details，不解析展示文本中的头；真实片段在 content。UI 可显示归档占位符和片段，但不能构造路径或自行读取归档。Agent 授权召回仍使用既有工具，没有 UI 直接召回方法。缺少/未知 details 或工具错误保留普通输出。归档可能敏感且不可信，不能作为 UI 指令执行。计划视图可以隐藏成功的后台工具卡片，同时保留完整历史/详情，这是展示策略。

### 进程内观察者与生命周期
同一 Pi 进程内的 TypeScript 适配器不加载 pi-ui 也能订阅：
~~~ts
import { FRONTEND_EVENT, type FrontendEventMessage } from "/path/to/my-extensions/sol-pi/frontend.ts";
const stop = pi.events.on(FRONTEND_EVENT, (raw: unknown) => {
  const { context, event } = raw as FrontendEventMessage;
  if (event.name.startsWith("context.")) {
    // 用 context.sessionManager.getSessionId() 关联会话，再由自己的UI展示。
  }
});
~~~

此进程内回调接收 {context,event}；远程客户端只收到上述传输中的 {name,data}。目前没有用于修改计划的公共 Sol-Pi getCapability 服务。按源后端/会话隔离更新，切换时清理过期组件，销毁时解除监听；取消和生成生命周期归能力负责。远程 UI 关闭进度组件不会取消 OCC 工作。

**最小接入：**普通工具/历史查看器 → 活动分支计划投影 → 可选事实通知 → 片段 details。不需要专属控制项、答案格式或修改响应。v1 entry/prefix 名称表示已支持结构；不能假定未知未来版本兼容。
