# Worker 子任务

[English](README_EN.md) · [安装](../README.md)

## 单模块复制与注册

复制两个完整目录；无需 shared、构建或生成辅助文件。Pi 1.5.2 提供 SDK 依赖，使用 Node 22.19+ 和自己的认证、模型配置。
~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/worker" "$DEST/ui/worker"
cp -R "$SRC/worker/." "$DEST/worker/"
cp -R "$SRC/pi-ui/worker/." "$DEST/ui/worker/"
pi --no-extensions -e "$DEST/worker/index.ts" -e "$DEST/ui/worker/index.ts"
~~~
也可加载能力与自己的 UI 入口，勿同时重复注册两套 UI：
~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/worker/index.ts";
export default function (pi: ExtensionAPI) { registerUI(pi); }
~~~
UI 可选，执行与校验仍归能力层；不支持的交互必须不可用，不能视为同意。增加 `--mode rpc` 使用结构化 UI；通用发现/传输规则见 [pi-ui](../pi-ui/README.md)。

## 调度契约

[SKILL.md](SKILL.md) 定义委派策略。每次启动子进程前，`prompt.ts` 从实际模块所在目录注入 `agents/worker.md` 及 `agents/{mode}.md`（scout/implement/test/review/fix），不依赖 agent 目录中的固定安装路径；缺失即失败，复制时不可丢弃。配置和历史仍归 agent 目录。提示修改只影响后续任务；任务契约独立传入。

每任务必须提供 objective、mode、显式 `preset: fast|normal|deep` 和 `sessionMode: new|fork`，由调度模型选择，不需例行询问用户。可选 title 是简短有意义的标签；空缺时显示完整 objective，不改变执行目标。
~~~js
worker({ tasks: [{ mode: "scout", objective: "Trace the call chain", preset: "fast", sessionMode: "fork" }] })
worker({ batchId: "<returned batchId>" })
worker({ batchId: "<returned batchId>", cancel: true })
~~~
- 启动接受 1–12 项 tasks，不接受旧 task；继续/取消必须有 batchId，cancel 只能为 true。未知或混用字段在接收前拒绝。每会话仅一个未完成批次：二次启动返回 blocked、active_batch、activeBatchId，不取消现有工作；并行任务放同一个数组。
- 启动/继续等待整个批次，无轮询、列表或仅状态模式；继续不创建任务或重载路由。响应在压缩 result:{status,results:[…]} 外保留 batchId、tasks[].taskId、finished。结果按输入顺序，一项失败/阻塞不取消其他任务；每会话最多保留 64 批次。
- 取消中止排队/运行任务并等待清理释放锁；取消已完成批次无害。中止活动的启动/继续调用也取消批次；预先中止的启动不接收任务，预先中止的继续取消已有任务。返回后中止无效；取消调用的清理本身不可再被中断。
- 缺信息、决策或授权即返回 blocked。子 Worker 禁止再次委派或与用户沟通；主 Agent 处理阻塞并审核证据、完整 diff 后交付。失败不自动重试。范围变化须取消、清理并派发新完整契约。

## 会话、路径与生命周期

new 使用 --no-session。fork 在批次接收时复制父会话活动分支，含已落定上下文、压缩和分支摘要，不含尚未记录的流式输出或其他分支；排队任务仍使用该快照，内存父会话也支持，不改父 entries/leaf/file。上下文任务优先 fork；历史敏感内容会发送给所选模型。子任务获得新 ID 的私有临时会话（0600），结束/启动失败/超时/取消时删除；SIGKILL 可能留下敏感文件。历史不扩大授权：契约、写入检查、递归禁令、--no-skills、--no-context-files 仍生效。

- 写模式必须声明 allowedPaths，相对 task.cwd：file.ts 为精确文件（可新建），dir/** 为目录后代；forbiddenPaths 同理。已有裸目录和尾斜杠在任何任务启动前拒绝，文件名空白有意义。relevantFiles 仅为读取提示，可在 cwd 外，不授予写权限。
- 子进程显式加载模块内的 `guard-entry.ts`，不依赖全局设置或自动发现；该入口只安装写入守卫，不暴露 Worker 调度工具，与完整 Worker 重复加载时按宿主去重。缺少或无效任务策略时 edit/write 拒绝执行。
- edit/write 必须同时满足词法与规范路径；禁止路径匹配两者。显式别名前缀可用，glob 内符号链接不能扩权。冲突锁规范化 cwd、符号链接及全部允许路径，排除项不缩小锁范围；只读任务因未声明完整读集也与写任务冲突。
- 共享 Git 工作区、路径锁和 edit/write 检查**不是文件系统/bash 沙箱，也不是跨窗口锁**。保护已有修改。重叠写任务的宿主 changed_files 按声明范围归因，observed_changed_files 保留原始 Git 差异，风险说明不确定归因；主 Agent 必须审核完整 diff。
- 会话持有的并发槽与锁直到进程终止及结果校验完毕才释放。独立任务可绕过冲突排队项，但不能越过更早冲突任务。降低并发不打断运行任务；固定总截止时间包括 Git 检查和校验。
- Unix 使用有界进程组 TERM/KILL 清理，包括主进程退出后的后代及辅助 Git 命令；并行 Git 检查等待全部收敛，非零退出使证据无效。Windows 仅终止直接子进程。会话关闭/替换/树导航清理而非恢复任务，不影响其他会话的运行时。

## 设置与进度

可选 UI 的 /worker_settings 或同进程能力 API 编辑配置。worker-settings.json 位于配置的 agent 目录。必须配置 fast/normal/deep 模型路由，**没有可移植默认模型 ID**，无关键词推断、auto 档位、启用开关或 manual 参数。运行默认项：并发 **3**、超时 **900000 ms**、输出 **65536 字节**。Thinking 原样使用 off/minimal/low/medium/high/xhigh/max，须模型支持；换模型不自动换 Thinking，也不静默降级。旧顶层 max 路由迁移，automaticDelegationEnabled 废弃项删除。

通过本模块 capabilities.ts/api.ts 按需发现 worker:v1，提供 getSettings/getModels/saveSettings/getSession/readTranscript。会话 lookup/history 返回脱离运行时的快照，subscribe 事件为 changed/history/batch/closed；关闭/替换时取消订阅，不跨会话保留所有权。观察者不能阻止清理或接管执行。

## 第三方 UI

先发现 source=extension 的 worker_settings 与 pi-ui-rpc，再发送命令；缺失能力/命令则禁用入口。不发送未知斜杠命令（可能成为模型输入）。**没有 worker.start、worker.cancel、worker.settings.get、worker.settings.save RPC**。Worker 是 Agent 工具，不是 /worker 命令；Pi abort 不是指定批次控制 API。

进度读取 tool_execution_update.partialResult.details、tool_execution_end.result.details 及历史 toolResult.details：
~~~json
{"kind":"worker-ui","batchId":"batch-1","originToolCallId":"call-1","revision":3,"snapshotAt":1730000001000,"startedAt":1730000000000,"limit":2,"total":1,"completed":0,"tasks":[{"index":0,"mode":"scout","objective":"Inspect the project","status":"running","requestedPreset":"normal","sessionMode":"fork","attempt":1,"phase":"Waiting for a slot","phaseCode":"waiting_slot","activities":[],"toolCalls":0,"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"contextTokens":0,"turns":0},"transcript":{"version":1,"id":"11111111-1111-4111-8111-111111111111"}}]}
~~~

按 kind=worker-ui、batchId/originToolCallId/revision 合并活动分支最新快照（worker-snapshot-v1 自定义条目的 data 同形），忽略过期 revision/废弃分支，不猜标签。任务 status 为 queued/running/completed/blocked/failed；逻辑使用稳定 phaseCode（如 waiting_slot、cleanup、cancelling、timed_out），不解析翻译文案。未知码仍可显示，未知 details 回退通用工具。时间戳为 epoch ms，历史计时冻结于 snapshotAt/finishedAt；activities/usage 是有界摘要而非全文记录。原卡片显示进度与 controlErrors，继续/取消卡片不重复显示，未知批次错误单独保留；渲染失败不能悬挂执行。

### 设置事务

发现后打开：
~~~json
{"id":"settings-open","type":"prompt","message":"/worker_settings"}
~~~

~~~json
{"type":"extension_ui_request","id":"settings-1","method":"input","title":"pi-goui:worker-settings:v1","placeholder":"{\"config\":{\"version\":2,\"fast\":{\"model\":\"provider/model\",\"thinking\":\"low\"},\"normal\":{\"model\":\"provider/model\",\"thinking\":\"medium\"},\"deep\":{\"model\":\"provider/model\",\"thinking\":\"high\"},\"maxConcurrentWorkers\":2,\"defaultTimeoutMs\":300000,\"maxOutputBytes\":65536},\"original\":{\"version\":2,\"fast\":{\"model\":\"provider/model\",\"thinking\":\"low\"},\"normal\":{\"model\":\"provider/model\",\"thinking\":\"medium\"},\"deep\":{\"model\":\"provider/model\",\"thinking\":\"high\"},\"maxConcurrentWorkers\":2,\"defaultTimeoutMs\":300000,\"maxOutputBytes\":65536},\"models\":[{\"id\":\"provider/model\",\"thinking\":[\"off\",\"low\",\"medium\",\"high\"]}]}"}
~~~

解析 placeholder：config 为草稿，original 为已提交基线，models 给出精确 ID/Thinking 选项，error 可选。示例模型只是占位符，不是默认值。返回**整个配置的 JSON 字符串**，不加包装：
~~~json
{"type":"extension_ui_response","id":"settings-1","value":"{\"version\":2,\"fast\":{\"model\":\"provider/model\",\"thinking\":\"low\"},\"normal\":{\"model\":\"provider/model\",\"thinking\":\"medium\"},\"deep\":{\"model\":\"provider/model\",\"thinking\":\"high\"},\"maxConcurrentWorkers\":2,\"defaultTimeoutMs\":300000,\"maxOutputBytes\":65536}"}
~~~

~~~json
{"type":"extension_ui_response","id":"settings-1","cancelled":true}
~~~

version=2，字段必须恰好如示例，每档只有 model/thinking。整数范围：并发 1–16、超时 1000–3600000 ms、输出 8192–1048576 字节。后端 schema、模型/Thinking 校验及原子保存具有权威性，UI 不直接写文件。无效提交以新 id 重开，保留草稿并替换请求 id；error 为 {code,field?,model?,thinking?,message?}，码为 invalid-config/invalid-number/model-unavailable/thinking-unsupported/save-failed。变更成功保存才通知 pi-goui:event:v1: + {"name":"worker.settings-saved","data":{}}，无变化不发；命令失败可通知 worker.settings-error {message}。普通 prompt 响应仅确认命令，不返回配置。

### 只读记录 RPC

只使用宿主任务的 transcript:{version:1,id:UUID}，绝不使用模型返回的路径；可选 incomplete/error 标识 size_limit/io_error/protocol_error。
~~~json
{"id":"rpc-1","type":"prompt","message":"/pi-ui-rpc {\"id\":\"read-1\",\"method\":\"worker.transcript\",\"params\":{\"descriptor\":{\"version\":1,\"id\":\"11111111-1111-4111-8111-111111111111\"}}}"}
~~~

外层 rpc-1 仅确认命令；另一条 extension_ui_request/method notify 的 message 为 pi-ui:rpc:v1: 加：
~~~json
{"id":"read-1","result":{"cursor":"22222222-2222-4222-8222-222222222222","reset":true,"records":[{"type":"worker_transcript_header","version":1,"id":"11111111-1111-4111-8111-111111111111","cwd":"/project","entries":[]},{"type":"message_end","message":{"role":"user","content":"Inspect the project","timestamp":1730000000000}},{"type":"worker_transcript_end","status":"completed","complete":true}],"more":false,"partial":false}}
~~~

关联内层 read-1，忽略通知 id。下次 params 为 `{descriptor:{version:1,id:"…"},cursor:"RETURNED_CURSOR"}`。后端校验 UUID/版本/路径及游标；不得制造偏移/游标或跨记录复用。单次最多读 4 MiB，游标闲置五分钟过期，重载/淘汰可 reset。
- reset:true 先丢弃旧回放再应用有序 JSON 对象 records。头包含 version/id/cwd/entries，随后为正常 Pi 事件；新 entries 为空，旧继承条目可读但不作为本任务内容显示。
- more:true 表示已有未读字节，应继续；more:false 不代表任务完成。需要时有界间隔轮询。partial:true 表示后端保留不完整 LF 行，不是完整事件；终止记录、关闭查看器或所有者后端/会话变化时停止。
- 错误为 {id,error:{code,message}}，含 unavailable/invalid-transcript/transcript-not-found/transcript-permission/transcript-read-failed/response-too-large。保留已显示消息并标注缺失，回退任务快照，**不自行读取私有文件**；显式重试，不无限循环。关闭/取消查看器不会取消 Worker。

## 记录隐私与保留

记录是独立只追加 UTF-8/LF 事件文件，**不是原生会话**，禁止传给 --session、恢复执行或交另一写入 Agent 打开。根目录为 path.join(path.dirname(agentDir()),"worker-transcripts")，通常 ~/.pi/worker-transcripts；agentDir 取 PI_CODING_AGENT_DIR 或 SDK getAgentDir。UUID 文件独占创建为 0600，根目录 0700；拒绝符号链接根、文件及逃逸。私有权限不等于加密。

仅保留本任务真实用户/助手/工具消息（含完整参数、结果、图片、流式增量和生命周期/重试/压缩/自定义事件），不复制继承 fork 历史、系统提示或环境转储。两种模式新头均 entries:[]。SDK 未发用户事件时，在输出/结束前补一次精确任务提示；这是预期输入，不证明失败启动实际执行过。未知诊断不是第二协议，损坏 JSON 标记不完整；跨块 UTF-8 和 LF 尾行不能当作完整事件。

终止 status 为 completed/failed/cancelled/timed_out/incomplete；complete:true 只表示录制正常结束，不等于验收成功。缺终止记录可能仍运行或已中断，绝不证明成功；刷新失败以最新终止标记/描述符错误为准。有界同步顺序追加，无无限写队列，正常结束 fsync；录制 IO 失败不使 Worker 失败/取消。单记录 **64 MiB**，预留终止标记空间，超限明确报告 size_limit，不伪装完整历史。

记录跨完成、取消、会话切换保留。仅新根/文件 ENOENT 才回退旧 agentDir()/worker-transcripts，危险/不可读/损坏文件不回退；游标打开新副本后，即使删除也不能重放旧同 ID 副本。旧记录不迁移、不自动删除、不计入新预算。无描述符不能重建，查看器须容忍文件被回收。

保留采用 **10 GiB 软预算**，统计规则 UUID JSONL 文件；不超预算不按年龄/数量删除，超预算才按 mtime 最旧优先（UUID 打破平局）删除合格已完成/死所有者记录，并复核身份和活动状态。启动/结束触发异步有界扫描；持久 0600 .retention.lock 的 mtime 节流 24 小时，失败也保留时间戳。无每日到期、常驻定时器或全文扫描（旧记录尾部最多 4 KiB）；过期锁接管仅尽力而为，不是分布式协调。

独占 UUID.active 标记保护活宿主直到任务执行结束，即使提前达到录制上限。PID 加 Linux boot/start 标识防 PID 复用；其他平台保守保留复用的活 PID。宿主 finally 在异常/取消时释放或结束租约；死所有者以后可回收，畸形/不可验证标记保守占用空间，扫描清理孤立非活动租约。标记只防删除，不阻塞新任务/查看器；SIGKILL 可留残余。清理仅超预算才删日志，避开链接/未知名称，活动或不确定数据可能使软预算暂时超限。
