# 会话自动命名

[English](README_EN.md)

## 只复制本模块与对应 UI

仅需复制以下**两个完整目录**并保持相对布局，不需要 `shared/`、整个仓库、顶层 `pi-ui/index.ts` 或构建步骤；必要的薄协议辅助代码已随目录附带。

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/autoname" "$DEST/ui/autoname"
cp -R "$SRC/autoname/." "$DEST/autoname/"
cp -R "$SRC/pi-ui/autoname/." "$DEST/ui/autoname/"
~~~

~~~text
my-extensions/
├── autoname/index.ts        # 能力及目录内全部文件
└── ui/autoname/index.ts     # 可选 UI 及目录内全部文件
~~~

**原生 Pi：直接加载两个入口：**

~~~sh
pi --no-extensions -e "$DEST/autoname/index.ts" -e "$DEST/ui/autoname/index.ts"
~~~

**也可由使用者自行注册原生 UI**，例如在 `$DEST/my-ui.ts` 中：

~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/autoname/index.ts";
export default function (pi: ExtensionAPI) {
  registerUI(pi); // 不注册或替换能力本身
}
~~~

此时加载能力入口与 `my-ui.ts`，不要再加载上述 UI 入口或整包适配器；若能力已自动发现，只补 UI 注册。能力缺少 UI 仍负责执行/校验，不支持的交互应返回不可用，不能默认同意。

RPC 使用同一组入口，增加 `PI_UI_PROTOCOL=1` 与 `--mode rpc`；完整消息格式见下文。Worker/Codex UI 各自附带可选方法桥，组合时只注册一次，其他模块不凭空增加方法。

Pi 1.5.2 提供 SDK、`pi-tui`、`typebox`，这两个目录运行不需要复制本仓库的 `node_modules`。认证、模型与能力配置仍由使用者自行设置，复制源码不代表零配置。保持上述目录名和相对层级，改变布局须自行调整导入。辅助文件同步仅供仓库维护者使用，**使用者无需生成代码**。

在主会话（非 Worker）稳定结束后生成名称，保留手动名称；模型选择、冷却和过期结果检查由能力负责，不以是否有 UI 为条件。此可选后台工作可能调用用户配置的模型并产生正常费用。

## 第三方 UI 快速接入（独立指南）
~~~sh
PI_UI_PROTOCOL=1 pi --mode rpc --no-extensions \
  -e /path/to/my-extensions/autoname/index.ts \
  -e /path/to/my-extensions/ui/autoname/index.ts
~~~
使用自己的 Pi 认证与设置。保留两个复制目录内的全部文件，不要只复制 index.ts。基线为 Node 22.19+ 与当前验证过的 Pi 1.5.2。PI_UI_PROTOCOL=1 启用结构化扩展 UI；兼容历史 PI_GOUI_PROTOCOL=1 别名及 pi-goui:* 消息名称，不需要 Go 前端。旧版适配器若尚不识别 PI_UI_PROTOCOL，可把该环境变量替换为 PI_GOUI_PROTOCOL=1，以下消息格式完全相同。

不用 RPC 传输时只加载能力即可执行，但只发进程内事实；可选 pi-ui 负责把事实转成 RPC/原生展示。此扩展**不注册斜杠命令、工具或远程控制方法**。

### 安全发现与接收事实
可以发送 {"id":"commands-1","type":"get_commands"} 查看后端命令，但不能据其列表证明被动能力是否存在。不要发送编造的 /autoname 或 pi-ui-rpc autoname.get 作为探针。没有事实到达时保持普通 Pi UI，不需要常驻专属控制项。

结构化模式下收到：
~~~json
{
  "type": "extension_ui_request",
  "id": "notice-1",
  "method": "notify",
  "notifyType": "info",
  "message": "pi-goui:event:v1:{\"name\":\"autoname.renamed\",\"data\":{\"name\":\"Inspect API behavior\",\"contextTokens\":4200,\"durationMs\":1250}}"
}
~~~

只剥离精确 pi-goui:event:v1: 前缀，再解析 {name,data}；这是兼容历史名称，非 Go UI 也使用它。外层 notifyType 是传输元数据，不是扩展的权威严重级别。notify 不回复 extension_ui_response。
~~~js
// Attach to your JSONL parser for Pi stdout; msg is one decoded record.
function factFromPi(msg) {
  if (msg.type !== "extension_ui_request" || msg.method !== "notify") return;
  const prefix = "pi-goui:event:v1:";
  if (typeof msg.message !== "string" || !msg.message.startsWith(prefix)) return;
  try {
    const event = JSON.parse(msg.message.slice(prefix.length));
    return event.name === "autoname.renamed" ? event.data : undefined;
  } catch { return undefined; }
}
~~~
## 配置与数据
用户的 autoname.json 支持 enabled、notify、cooldownSeconds、model、reasoning；默认 {"enabled":true,"notify":true,"cooldownSeconds":600,"model":"auto","reasoning":"minimal"}，model 可为 auto 或 provider/model。能力在稳定回合后重读自己的配置。UI 不应打开此私有配置另写一套控制逻辑。

name 为字符串，contextTokens 为输入上下文 token 估算，durationMs 为毫秒耗时。仅在 notify=true 且名称已提交时发通知。没有通知不能证明失败或未安装；事件不是可编辑草稿，也不是需要回复确认的命名请求。

标准 Pi RPC 可读取权威名称：
~~~json
{
  "id": "state-1",
  "type": "get_state"
}
~~~

标准响应 data 包含已设置时的 sessionName、sessionId 和其他 Pi 状态。页面激活及普通会话元数据变化时刷新；实时事实可触发一次 get_state，也能恢复断线期间错过的命名。不要根据无关模型事件的到达顺序推断命名完成。

用户明确手动命名时使用 Pi 的**标准** set_session_name，不要编造扩展方法：
~~~json
{
  "id": "rename-1",
  "type": "set_session_name",
  "name": "My deliberate name"
}
~~~

只在明确用户操作时调用，不能每收到 autoname 事实就自动写回。标准 response 表示成功/失败；命名能力会保留手动名称，不再覆盖。

## 同进程适配（不加载 pi-ui）
只有运行在同一 Pi 进程内的扩展才能订阅该总线：
~~~ts
import { FRONTEND_EVENT, type FrontendEventMessage } from "/path/to/my-extensions/autoname/frontend.ts";
const unsubscribe = pi.events.on(FRONTEND_EVENT, (raw: unknown) => {
  const { context, event } = raw as FrontendEventMessage;
  if (event.name === "autoname.renamed") {
    // Render event.data for context.sessionManager.getSessionId().
  }
});
~~~
此处载荷为 {context,event}；远程只有 {name,data}。没有可调用的 getCapability 服务。适配器销毁时解除监听；UI 观察者故障不能影响能力结果。

## 生命周期、错误与最小实现
按源后端/会话路由事件，替换会话时清理过期提示；未知事件名/版本及畸形数据应忽略，不显示原始协议 JSON。通知历史不会自动回放；断线或关闭提示不能回滚能力已经完成的工作。不要编造“取消操作”API。

缺少适配器时保留普通 RPC notify/工具/会话行为。不启用结构化标记的通用 RPC 不会导出这些机器可读事实，应使用标准 Pi 状态/结果；原生 Pi 可以收到格式化通知。

**最小实现：**标准 Pi UI → 可选事实解析 → 检查预期数据类型 → 显示/刷新 → 释放会话所属监听。展示该事件不需要文件访问、认证、后端模块导入、确认回答或修改接口。
