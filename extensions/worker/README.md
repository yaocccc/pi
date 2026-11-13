# Worker 子任务

[English / 完整协议](README_EN.md) · [安装](../README.md)

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

Worker 在独立 Pi 子进程中执行有边界的任务。需要自己的模型与认证，以及当前 Pi agent 目录内有效的 `worker-settings.json`。可加载 pi-ui 后通过 `/worker_settings` 配置 fast / normal / deep 三档模型、Thinking、并发、超时与输出上限。不要复制作者的模型 ID。

## 安全与生命周期

- 每项任务显式给出 `sessionMode: new | fork` 和 `preset: fast | normal | deep`；由调度模型选择。
- new 不继承父会话；fork 在批次接收时复制当前活动分支，可能将历史敏感内容发送给所选模型。
- 隔离的是会话，不是 Git 工作区。路径检查不是操作系统沙箱，bash 也不是被完整沙箱化。
- 写任务需要 allowedPaths；相关文件只授予读取提示。子任务不能再次委派。
- 启动、续等和取消均由能力层处理；失败不自动重试。交付前等待或取消未完成批次。
- 注入提示来自 `agents/worker.md` 和角色文件；不能只复制 index.ts。
- fork 临时会话正常结束时清理；SIGKILL 等不可捕获退出可能留下敏感临时数据。

`worker:v1` 提供会话快照、历史、变化订阅、设置读取、可用模型和权威保存。菜单只消费这些接口；成功保存事实由能力发布。状态协议使用稳定 `phaseCode`，不根据中文文案推断任务状态。

## 只读任务记录

记录通常在 `~/.pi/worker-transcripts/`，即配置的 agent 目录旁，而不是项目源码内。它们不是可恢复会话；保留当前任务消息/工具事件，不包含继承的 fork 历史或系统提示。单任务上限 64 MiB，总量软预算 10 GiB，生命周期触发的回收有间隔且保护活动任务。详情见 [TRANSCRIPT.md](TRANSCRIPT.md)。

能力的 `readTranscript` 负责校验描述符、路径、游标并限量读取；pi-ui 的 `worker.transcript` 仅桥接方法。Go 第三方适配器消费 RPC 结果，不猜测私有文件布局。没有记录或能力时只能显示已有任务结果，不能重建历史。

## 验证

从仓库根目录执行 `npm --prefix extensions test` 与 `npm --prefix extensions run typecheck`。主要协议、并发、边界、回放和设置测试均在源码中；它们不是对所有真实子进程/模型的端到端保证。完整角色和验收流程见英文文档与 [SKILL.md](SKILL.md)。

## 第三方 UI 快速接入（独立指南）

### 启动、前提与发现
~~~sh
PI_UI_PROTOCOL=1 pi --mode rpc --no-extensions \
  -e /path/to/my-extensions/worker/index.ts \
  -e /path/to/my-extensions/ui/worker/index.ts
~~~
使用自己的 Pi 认证与设置。保留两个复制目录内的全部文件，不要只复制 index.ts。基线为 Node 22.19+ 与当前验证过的 Pi 1.5.2。PI_UI_PROTOCOL=1 启用结构化扩展 UI；兼容历史 PI_GOUI_PROTOCOL=1 别名及 pi-goui:* 消息名称，不需要 Go 前端。旧版适配器若尚不识别 PI_UI_PROTOCOL，可把该环境变量替换为 PI_GOUI_PROTOCOL=1，以下消息格式完全相同。

先在自己的 worker-settings.json 配置模型/档位；UI 不应直接写这个文件，也不能假定作者的模型 ID 存在。真实 Worker 执行仍有正常权限和费用。下例是 UI 读取/设置，不会要求模型启动 Worker。

先发送标准 Pi 请求，以 id 匹配响应：

~~~json
{
  "id": "discover",
  "type": "get_commands"
}
~~~

检查 response.data.commands 中所需命令的 source 是否为 "extension"；同名提示模板或 Skill 不算。缺失时隐藏入口，**不要发送未知斜杠命令**，否则 Pi 可能将其当成模型输入。命令发现只确认适配器，不保证其下层每个能力都存在。
设置对话框使用 worker_settings，记录读取使用 pi-ui-rpc。当前**没有远程 worker.start、worker.cancel、worker.settings.get、worker.settings.save 方法**。worker 是 Agent 工具，不是斜杠命令，不能发送 /worker 或 /worker.cancel。工具启动/控制留在 Pi/Agent。标准 Pi abort 取消当前 Agent 运行，不是指定 Worker 批次的控制 API。

### 先显示工具进度，无须额外方法
监听 toolName="worker" 的 tool_execution_start/update/end，读取 update.partialResult.details 和 end.result.details；历史 toolResult 消息也有 details。识别 kind="worker-ui"，例如：
~~~json
{
  "kind": "worker-ui",
  "batchId": "batch-1",
  "originToolCallId": "call-1",
  "revision": 3,
  "snapshotAt": 1730000001000,
  "startedAt": 1730000000000,
  "limit": 2,
  "total": 1,
  "completed": 0,
  "tasks": [
    {
      "index": 0,
      "mode": "scout",
      "objective": "Inspect the project",
      "status": "running",
      "requestedPreset": "normal",
      "sessionMode": "fork",
      "attempt": 1,
      "phase": "Waiting for a slot",
      "phaseCode": "waiting_slot",
      "activities": [],
      "toolCalls": 0,
      "usage": {
        "input": 0,
        "output": 0,
        "cacheRead": 0,
        "cacheWrite": 0,
        "contextTokens": 0,
        "turns": 0
      },
      "transcript": {
        "version": 1,
        "id": "11111111-1111-4111-8111-111111111111"
      }
    }
  ]
}
~~~

这是 detached snapshot，不是 patch。以“后端/会话 + batchId”标识卡片，并绑定 originToolCallId；后续 continuation 调用不应生成另一张启动卡片。丢弃更旧 revision（旧数据可回退 snapshotAt），不要把迟到事件接到新选中的会话。没有 details 时保留最后有效结果。

startedAt/finishedAt/snapshotAt、活动 at 为毫秒时间戳；limit/total/completed 是批次计数。任务 status 为 queued/running/completed/blocked/failed；index 是批内序号。任务还可包含 title、modelId、thinking、resolvedPreset、时间戳和 result。activities 项含 id、type（phase/tool/thinking）、status、label、at，可有 phaseCode/detail/contextSizeEstimate。usage 包含数值 input/output/cacheRead/cacheWrite/contextTokens/turns。controlErrors 为 {toolCallId,message} 数组；payload 是模型结果，不是卡片渲染前提。

phase、活动 label 只是文案。逻辑使用 phaseCode：waiting_slot/slot_acquired 表示槽位活动；其他当前值为 queued、running、routing、snapshot_workspace、workspace_snapshotted、starting、validating、cleanup、cancelling、terminating_timeout、terminating_output、completed、blocked、failed、cancelled、timed_out。未知/缺失 code 仍可展示，不解析中文。历史耗时冻结在 snapshotAt/finishedAt。未知 details 版本回退普通工具显示。

通过 get_entries/get_tree 恢复历史时，只投影**活动分支**。customType="worker-snapshot-v1" 的可选 custom entry 在 data 放相同 details；必须匹配原始 worker tool call 与 batch，不能混合废弃分支或按标题猜关联。

### 设置 UI：一次结构化 input 事务
发现 worker_settings 后发送 {"id":"settings-command","type":"prompt","message":"/worker_settings"}；命令会等待空闲。结构化请求如下：
~~~json
{
  "type": "extension_ui_request",
  "id": "settings-1",
  "method": "input",
  "title": "pi-goui:worker-settings:v1",
  "placeholder": "{\"config\":{\"version\":2,\"fast\":{\"model\":\"provider/model\",\"thinking\":\"low\"},\"normal\":{\"model\":\"provider/model\",\"thinking\":\"medium\"},\"deep\":{\"model\":\"provider/model\",\"thinking\":\"high\"},\"maxConcurrentWorkers\":2,\"defaultTimeoutMs\":300000,\"maxOutputBytes\":65536},\"original\":{\"version\":2,\"fast\":{\"model\":\"provider/model\",\"thinking\":\"low\"},\"normal\":{\"model\":\"provider/model\",\"thinking\":\"medium\"},\"deep\":{\"model\":\"provider/model\",\"thinking\":\"high\"},\"maxConcurrentWorkers\":2,\"defaultTimeoutMs\":300000,\"maxOutputBytes\":65536},\"models\":[{\"id\":\"provider/model\",\"thinking\":[\"off\",\"low\",\"medium\",\"high\"]}]}"
}
~~~

将 placeholder 再解析为 JSON。config 是编辑草稿，original 是初始已提交状态，models 提供确切模型 ID 与合法 thinking，error 可选。例中的 provider/model 仅作示意，应使用实际 models 列表。把**整个配置直接 JSON 编码成 value 字符串**返回，不要包成 {config:…}：
~~~json
{
  "type": "extension_ui_response",
  "id": "settings-1",
  "value": "{\"version\":2,\"fast\":{\"model\":\"provider/model\",\"thinking\":\"low\"},\"normal\":{\"model\":\"provider/model\",\"thinking\":\"medium\"},\"deep\":{\"model\":\"provider/model\",\"thinking\":\"high\"},\"maxConcurrentWorkers\":2,\"defaultTimeoutMs\":300000,\"maxOutputBytes\":65536}"
}
~~~

version 必须为 2，字段恰好为 version、fast、normal、deep、maxConcurrentWorkers、defaultTimeoutMs、maxOutputBytes。每档恰好含 model 和 thinking。整数范围：并发 1–16，超时 1,000–3,600,000 毫秒，输出 8,192–1,048,576 字节。最终模型/Thinking 校验归后端，前端提示不能代替权威校验。

无效提交会以新请求 id 重新打开 input，携带 error {code,field?,model?,thinking?,message?}。code 为 invalid-config、invalid-number、model-unavailable、thinking-unsupported、save-failed。重试时更换活动 id，保留后端草稿。有变化且成功的保存通过 notify 发布 pi-goui:event:v1: + {"name":"worker.settings-saved","data":{}}；无变化不发该事实。命令级失败可发 worker.settings-error，data 为 {message}。普通 prompt response 只确认/完成命令，不返回配置对象。

取消：
~~~json
{
  "type": "extension_ui_response",
  "id": "settings-1",
  "cancelled": true
}
~~~
不启用结构化标记时，适配器使用普通 select/input；通用 RPC UI 无须自定义设置页。

### 可选只读记录查看器
仅使用任务自带 transcript {version:1,id}；额外 incomplete/error 可表示 size_limit/io_error/protocol_error。id 是不透明 UUID，不是路径或可恢复会话。发现 pi-ui-rpc 后：
~~~json
{
  "id": "rpc-1",
  "type": "prompt",
  "message": "/pi-ui-rpc {\"id\":\"read-1\",\"method\":\"worker.transcript\",\"params\":{\"descriptor\":{\"version\":1,\"id\":\"11111111-1111-4111-8111-111111111111\"}}}"
}
~~~

普通 id=rpc-1 的 response 只是命令确认；另一个 method=notify 的 extension_ui_request 会在 message 中携带 "pi-ui:rpc:v1:" 加以下 JSON：
~~~json
{
  "id": "read-1",
  "result": {
    "cursor": "22222222-2222-4222-8222-222222222222",
    "reset": true,
    "records": [
      {
        "type": "worker_transcript_header",
        "version": 1,
        "id": "11111111-1111-4111-8111-111111111111",
        "cwd": "/project",
        "entries": []
      },
      {
        "type": "message_end",
        "message": {
          "role": "user",
          "content": "Inspect the project",
          "timestamp": 1730000000000
        }
      },
      {
        "type": "worker_transcript_end",
        "status": "completed",
        "complete": true
      }
    ],
    "more": false,
    "partial": false
  }
}
~~~

用内层 id read-1 关联，不使用外层通知 id。下一次 params 为 {descriptor:{version:1,id:"…"},cursor:"RETURNED_CURSOR"}。不要计算偏移、编造游标或混用其他记录的游标。每次最多读取 4 MiB 文件字节，完整记录可跨越多次读取。游标缓存有界，空闲五分钟后过期；reload/淘汰可能导致重置。

reset:true 时先清空旧回放状态。records 是按顺序排列的 JSON 对象，**不是 JSON 字符串**：首先为含 version/id/cwd/entries 的 worker_transcript_header，然后是普通 Pi 事件对象（message_start/update/end、tool_execution_*、entry_appended 等），最后可能有 worker_transcript_end。新头部 entries:[]；旧头部可能带继承历史，不能把它展示成本次任务工作。end 含 status（completed/failed/cancelled/timed_out 或 incomplete）及 complete:boolean；不完整 end 可含 error。缺少 end 不是成功证据。

more:true 表示当次已有未读取字节，应继续读取；more:false 不代表任务完成，运行中需有界延时轮询。partial:true 表示后端保留了未完成 JSON 行，不是可渲染的“半条事件”。记录结束、查看器关闭或所属后端/会话变化后停止轮询。

RPC 错误形状为 {id,error:{code,message}}，包括 unavailable、invalid-transcript、transcript-not-found、transcript-permission、transcript-read-failed、response-too-large。保留已收到消息并标记不完整；没有消息/描述符/能力时显示已有任务快照，不能转而读本地私有文件。取消读取/关闭查看器只停止本地等待，不停止 Worker。失败后使用显式重试，不无限循环。

### 进程内前端替代方案
以下方法仅限同一 Pi 进程，不是新增远程方法：
~~~ts
import { getCapability } from "/path/to/my-extensions/worker/capabilities.ts";
import { WORKER_CAPABILITY, type WorkerCapability } from "/path/to/my-extensions/worker/api.ts";
const service = getCapability<WorkerCapability>(pi, WORKER_CAPABILITY); // "worker:v1"
if (service) {
  const config = service.getSettings();
  const choices = service.getModels(ctx); // [{id,thinking:[…]}]
  // 仅用户明确保存时：service.saveSettings(ctx, editedConfig)
  const view = service.getSession();
  const stop = view.subscribe(change => {
    // changed/history/batch/closed；batch 事件带 batchId。
    // 显示 view.lookup(selectedBatch) ?? view.history(selectedBatch)。
    // closed 后冻结本 view，释放计时器与监听器。
  });
}
~~~

view 提供 awaitingHistory/closed、detached lookup/history 快照。保留所属 view，新会话会得到另一个 view。readTranscript({descriptor,cursor?}) 返回上述形状。SettingsError 通过 data 提供结构化错误，不依赖翻译文案。可用 pi.events.on("pi:frontend-event:v1", ({context,event}) => …) 观察共享事实，筛选 event.name="worker.settings-saved"；context 标识所属会话。UI 不应再发第二次成功事实。

**最小 UI 顺序：**普通工具结果 → 快照卡片 → 设置事务 → 可选记录回放 → 所有者/revision/取消清理。v1 名称是版本标记，不能据此猜未来未知结构。
