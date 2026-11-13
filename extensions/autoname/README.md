# 自动会话命名

[English](README_EN.md)

主会话在 agent_settled 后按冷却窗口自动命名；不运行于 Worker。命名、模型请求和会话写入由能力负责，UI 只显示成功事实。

## 安装

复制两个完整目录并保持相对布局；已包含 helper，不需要 shared、node_modules、构建或作者的配置。Pi 提供 SDK 依赖。

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extension
mkdir -p "$DEST/autoname" "$DEST/ui/autoname"
cp -R "$SRC/autoname/." "$DEST/autoname/"
cp -R "$SRC/pi-ui/autoname/." "$DEST/ui/autoname/"
pi --no-extensions -e "$DEST/autoname/index.ts" -e "$DEST/ui/autoname/index.ts"
~~~

也可由自己的入口注册（此时不要再单独加载 UI 入口）：
~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/autoname/index.ts";

export default function (pi: ExtensionAPI) {
  registerUI(pi);
}
~~~

仅加载能力也会命名；通知需对应适配器。此模块没有专用工具、斜杠命令或远程调用方法。

## 配置与隐私

当前 Pi agent 目录下的 autoname.json 支持以下字段；文件缺失或无法读取时使用默认值，不自动创建或改写配置文件：
~~~json
{"enabled":true,"notify":true,"cooldownSeconds":600,"model":"auto","reasoning":"minimal"}
~~~

- model="auto" 使用当前会话模型，也可指定已配置的 provider/model；未指定 provider 时尝试唯一同名模型。必须自行配置认证。
- reasoning 可选 minimal/low/medium/high/xhigh/max；冷却从尝试开始计时，失败也消耗窗口。已有名称只在检测到话题切换时更新；请求期间的手动改名、分支/轮次变化和过期结果不会被覆盖。
- 命名会读取当前分支上下文、调用模型并可能额外计费。notify=false 仅关闭提示，不关闭命名；要停止请求使用 enabled=false。
- 模型/认证不可用或命名失败不阻断普通对话。能力保存名称和尝试记录，不直接访问终端、toast 或编辑器。

## 第三方 UI：名称与事实

普通 Pi RPC 的 get_state 响应包含当前 sessionName，这是恢复页面时的状态来源；不要依赖通知日志重建名称：
~~~json
{"id":"state-1","type":"get_state"}
~~~

加载对应 UI 后，成功命名且 notify=true 时会收到：
~~~json
{"type":"extension_ui_request","id":"notice-name-1","method":"notify","notifyType":"info","message":"pi-goui:event:v1:{\"name\":\"autoname.renamed\",\"data\":{\"name\":\"Review worker scheduling\",\"contextTokens\":2048,\"durationMs\":820}}"}
~~~

只剥离精确 pi-goui:event:v1: 前缀并解析 JSON。name 是会话名，contextTokens 是本次命名请求的上下文 token，durationMs 是耗时；后两项不是主会话总用量。通知无需 extension_ui_response，仅更新所属后端/会话；恢复历史不重复 toast，缺失通知不代表命名失败，畸形/未知事件忽略。

手动改名使用标准 Pi RPC 的 set_session_name，而不是虚构 /autoname：
~~~json
{"id":"name-1","type":"set_session_name","name":"My explicit title"}
~~~

同进程宿主可订阅 pi:frontend-event:v1 并按 event.name="autoname.renamed" 过滤 {context,event}；使用 context 关联归属，卸载时取消订阅。事实在写入后发布，观察者异常不改变命名结果。无适配器时保留标准会话名显示；不从前端读取 autoname.json。

配置与命名边界见 [config.ts](config.ts)、[helpers.ts](helpers.ts)、[index.ts](index.ts)；通用 UI 边界见 [pi-ui](../pi-ui/README.md)。
