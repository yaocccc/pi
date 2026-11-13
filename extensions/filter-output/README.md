# 工具输出过滤

[English](README_EN.md)

## 只复制本模块与对应 UI

仅需复制以下**两个完整目录**并保持相对布局，不需要 `shared/`、整个仓库、顶层 `pi-ui/index.ts` 或构建步骤；必要的薄协议辅助代码已随目录附带。

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/filter-output" "$DEST/ui/filter-output"
cp -R "$SRC/filter-output/." "$DEST/filter-output/"
cp -R "$SRC/pi-ui/filter-output/." "$DEST/ui/filter-output/"
~~~

~~~text
my-extensions/
├── filter-output/index.ts        # 能力及目录内全部文件
└── ui/filter-output/index.ts     # 可选 UI 及目录内全部文件
~~~

**原生 Pi：直接加载两个入口：**

~~~sh
pi --no-extensions -e "$DEST/filter-output/index.ts" -e "$DEST/ui/filter-output/index.ts"
~~~

**也可由使用者自行注册原生 UI**，例如在 `$DEST/my-ui.ts` 中：

~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/filter-output/index.ts";
export default function (pi: ExtensionAPI) {
  registerUI(pi); // 不注册或替换能力本身
}
~~~

此时加载能力入口与 `my-ui.ts`，不要再加载上述 UI 入口或整包适配器；若能力已自动发现，只补 UI 注册。能力缺少 UI 仍负责执行/校验，不支持的交互应返回不可用，不能默认同意。

RPC 使用同一组入口，增加 `PI_UI_PROTOCOL=1` 与 `--mode rpc`；完整消息格式见下文。Worker/Codex UI 各自附带可选方法桥，组合时只注册一次，其他模块不凭空增加方法。

Pi 1.5.2 提供 SDK、`pi-tui`、`typebox`，这两个目录运行不需要复制本仓库的 `node_modules`。认证、模型与能力配置仍由使用者自行设置，复制源码不代表零配置。保持上述目录名和相对层级，改变布局须自行调整导入。辅助文件同步仅供仓库维护者使用，**使用者无需生成代码**。

在模型消费前过滤部分成功工具结果：替换特定敏感文件读取内容，对文本应用常见秘密模式脱敏。它不是沙箱、全面秘密扫描器，也不保证所有显示和日志都已脱敏。

## 第三方 UI 快速接入（独立指南）
~~~sh
PI_UI_PROTOCOL=1 pi --mode rpc --no-extensions \
  -e /path/to/my-extensions/filter-output/index.ts \
  -e /path/to/my-extensions/ui/filter-output/index.ts
~~~
使用自己的 Pi 认证与设置。保留两个复制目录内的全部文件，不要只复制 index.ts。基线为 Node 22.19+ 与当前验证过的 Pi 1.5.2。PI_UI_PROTOCOL=1 启用结构化扩展 UI；兼容历史 PI_GOUI_PROTOCOL=1 别名及 pi-goui:* 消息名称，不需要 Go 前端。旧版适配器若尚不识别 PI_UI_PROTOCOL，可把该环境变量替换为 PI_GOUI_PROTOCOL=1，以下消息格式完全相同。

不用 RPC 传输时只加载能力即可执行，但只发进程内事实；可选 pi-ui 负责把事实转成 RPC/原生展示。此扩展**不注册斜杠命令、工具或远程控制方法**。

### 安全发现与接收事实
可以发送 {"id":"commands-1","type":"get_commands"} 查看后端命令，但不能据其列表证明被动能力是否存在。不要发送编造的 /filter-output 或 pi-ui-rpc filter-output.get 作为探针。没有事实到达时保持普通 Pi UI，不需要常驻专属控制项。

结构化模式下收到：
~~~json
{
  "type": "extension_ui_request",
  "id": "notice-1",
  "method": "notify",
  "notifyType": "info",
  "message": "pi-goui:event:v1:{\"name\":\"filter.redacted\",\"data\":{\"path\":\".env\"}}"
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
    return event.name === "filter.redacted" ? event.data : undefined;
  } catch { return undefined; }
}
~~~
## 范围与事件的准确含义
没有独立配置文件或公共控制服务。错误结果不处理；.env.example 读取明确直接放行。当前敏感文件模式包括 .env 及部分变体、.dev.vars、secrets.{json,yaml,yml,toml} 和 credentials 前缀路径；文本模式还覆盖部分常见凭据/私钥形态，但这些规则不能作为安全保证。

filter.redacted **只**携带 {path:string}，表示敏感文件读取内容被替换。不含原文、脱敏数量、tool-call ID 或严重级别。普通文本秘密模式替换目前**不发**此事件；没有通知不等于“未修改”或“安全”。

path 应作为不可信文本展示，不作为指令执行，也不自动打开本地文件。不要为了展开通知而重读文件或显示原文。继续呈现标准工具结果，不合成工具成功，也不按路径猜其对应调用。缺少/未知 details 时使用普通工具显示。

脱敏通知是事实，不是授权请求。没有回答格式、“显示原文”动作或 filter.disable RPC 方法；关闭提示只影响 UI。

## 同进程适配（不加载 pi-ui）
只有运行在同一 Pi 进程内的扩展才能订阅该总线：
~~~ts
import { FRONTEND_EVENT, type FrontendEventMessage } from "/path/to/my-extensions/filter-output/frontend.ts";
const unsubscribe = pi.events.on(FRONTEND_EVENT, (raw: unknown) => {
  const { context, event } = raw as FrontendEventMessage;
  if (event.name === "filter.redacted") {
    // Render event.data for context.sessionManager.getSessionId().
  }
});
~~~
此处载荷为 {context,event}；远程只有 {name,data}。没有可调用的 getCapability 服务。适配器销毁时解除监听；UI 观察者故障不能影响能力结果。

## 生命周期、错误与最小实现
按源后端/会话路由事件，替换会话时清理过期提示；未知事件名/版本及畸形数据应忽略，不显示原始协议 JSON。通知历史不会自动回放；断线或关闭提示不能回滚能力已经完成的工作。不要编造“取消操作”API。

缺少适配器时保留普通 RPC notify/工具/会话行为。不启用结构化标记的通用 RPC 不会导出这些机器可读事实，应使用标准 Pi 状态/结果；原生 Pi 可以收到格式化通知。

**最小实现：**标准 Pi UI → 可选事实解析 → 检查预期数据类型 → 显示/刷新 → 释放会话所属监听。展示该事件不需要文件访问、认证、后端模块导入、确认回答或修改接口。
