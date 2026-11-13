# Codex 档位能力

[English](README_EN.md) · [安装](../README.md)

## 只复制本模块与对应 UI

仅需复制以下**两个完整目录**并保持相对布局，不需要 `shared/`、整个仓库、顶层 `pi-ui/index.ts` 或构建步骤；必要的薄协议辅助代码已随目录附带。

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/codex-fast" "$DEST/ui/codex-fast"
cp -R "$SRC/codex-fast/." "$DEST/codex-fast/"
cp -R "$SRC/pi-ui/codex-fast/." "$DEST/ui/codex-fast/"
~~~

**原生 Pi：直接加载两个入口：**

~~~sh
pi --no-extensions -e "$DEST/codex-fast/index.ts" -e "$DEST/ui/codex-fast/index.ts"
~~~

**也可由使用者自行注册原生 UI**，例如在 `$DEST/my-ui.ts` 中：

~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/codex-fast/index.ts";
export default function (pi: ExtensionAPI) {
  registerUI(pi); // 不注册或替换能力本身
}
~~~

此时加载能力入口与 `my-ui.ts`，不要再加载上述 UI 入口或整包适配器；若能力已自动发现，只补 UI 注册。能力缺少 UI 仍负责执行/校验，不支持的交互应返回不可用，不能默认同意。

RPC 在上述入口命令增加 `--mode rpc`；见下方 wire 示例及[通用 RPC 契约](../pi-ui/README.md)。 可选方法桥只注册一次，与 Worker UI 组合时也不重复注册。

Pi 1.5.2 提供 SDK、`pi-tui`、`typebox`，这两个目录运行不需要复制本仓库的 `node_modules`。认证、模型与能力配置仍由使用者自行设置，复制源码不代表零配置。保持上述目录名和相对层级，改变布局须自行调整导入。

## 配置与接口

能力独立于 UI，负责配置校验、旧 `fast`/`ultrafast` 布尔值迁移、原子保存与 provider 请求的 `service_tier` 映射。缺省为 normal；不会改变不适用的 provider。使用自己的认证与模型，不需要作者的配置。

`codex-fast.json` 位于当前 Pi agent 目录，新配置形如 `{"level":"fast"}`，支持 normal / fast / ultrafast。仅处理 provider=openai-codex、api=openai-codex-responses 且载荷为非数组对象的请求：normal 不修改载荷（也不移除已有 service_tier）；fast 使用 priority；ultrafast 仅在模型 ID 精确等于 gpt-6-astra 时使用 ultrafast，其他模型回退 priority。显示始终反映配置档位。

通过 `getCapability<CodexFastService>(pi, CODEX_FAST_CAPABILITY)` 发现服务：

- `getConfig()` 重新读取持久化状态，返回独立快照。
- `levels` 提供不可变的支持档位列表。
- `setLevel(unknown)` 校验并原子保存，返回独立状态；无效档位抛出 `invalid-level`，保存失败不更新已提交内存状态。
- 成功保存发布 `codex-fast:changed` 的 `{level}` 事实。外部文件变化在下一次读取/请求时加载，不视为服务保存事件。

可选 pi-ui 提供 `/codex-fast`、原生菜单与 Go RPC 兼容。通用扩展 RPC 方法 `codex-fast.get` / `codex-fast.set` 分别读取能力状态和提交档位；Go 前端不读写此配置。缺少适配器不影响能力请求处理，缺少能力时前端应隐藏入口。服务在使用时发现，以容忍安装顺序。

## 第三方 UI 快速接入（独立指南）

### 启动与发现
要求 Node 22.19+、Pi 1.5.2。RPC 默认输出结构化消息，无需协议环境变量；`pi-goui:*` 名称不要求 Go 前端。

先发送标准 Pi 请求，以 id 匹配响应：

~~~json
{
  "id": "discover",
  "type": "get_commands"
}
~~~

检查 response.data.commands 中所需命令的 source 是否为 "extension"；同名提示模板或 Skill 不算。缺失时隐藏入口，**不要发送未知斜杠命令**，否则 Pi 可能将其当成模型输入。命令发现只确认适配器，不保证其下层每个能力都存在。
使用数据 API 时要求 pi-ui-rpc；使用可选设置对话框时要求 codex-fast。即使桥存在，本能力未加载时方法仍返回 error.code="unavailable"；应隐藏档位控件，不影响普通 Pi 功能。

### 经 Pi RPC 读取和设置
以下每个示例都是写入 Pi stdin 的一条 JSONL；按行解析 stdout，再按 type/id 路由，不需要额外 socket、文件访问或 TypeScript 导入：
~~~json
{
  "id": "rpc-1",
  "type": "prompt",
  "message": "/pi-ui-rpc {\"id\":\"tier-get-1\",\"method\":\"codex-fast.get\",\"params\":{}}"
}
~~~

普通 id 为 rpc-1 的 response 只是提示词确认（通常 data.disposition="handled"），**不是方法结果**。方法结果通过 notify 返回：
~~~json
{
  "type": "extension_ui_request",
  "id": "notification-1",
  "method": "notify",
  "notifyType": "info",
  "message": "pi-ui:rpc:v1:{\"id\":\"tier-get-1\",\"result\":{\"level\":\"normal\",\"levels\":[\"normal\",\"fast\",\"ultrafast\"]}}"
}
~~~

只剥离精确前缀 pi-ui:rpc:v1:，JSON.parse 剩余内容，用内层 id tier-get-1 关联请求。外层通知 id 无关。level 是后端确认的当前值，levels 是可选项；不要根据模型名或 provider 猜可用性。

保存请求：
~~~json
{
  "id": "rpc-2",
  "type": "prompt",
  "message": "/pi-ui-rpc {\"id\":\"tier-set-1\",\"method\":\"codex-fast.set\",\"params\":{\"level\":\"fast\"}}"
}
~~~

内层 result 仍为 {level,levels}；仅在成功后更新显示。权威校验、旧文件兼容、原子保存和请求档位选择均归后端。非法选项返回以下内层信封：
~~~json
{
  "id": "tier-set-1",
  "error": {
    "code": "invalid-level",
    "message": "Invalid Codex level: invalid"
  }
}
~~~

其他错误包括 unavailable、invalid-request、response-too-large、extension-error 或 EACCES 等 I/O code。失败时保留最后确认状态，显示错误，不假装保存成功；未知错误只作诊断消息。桥按二次编码后的大小限制响应，避免突破传输行上限。

结构化适配器启用时，保存成功还会发布事实：
~~~json
{
  "type": "extension_ui_request",
  "id": "notice-2",
  "method": "notify",
  "notifyType": "info",
  "message": "pi-goui:event:v1:{\"name\":\"codex.settings-saved\",\"data\":{\"level\":\"fast\"}}"
}
~~~

可据此刷新或标记显示过期，但不能代替请求响应关联。它不是覆盖所有外部文件修改或其他后端进程的推送源：切回页面时重新查询，如需实时同步可有界轮询。每个标签/后端的状态独立保存。

### 也可以使用设置对话框
也可走标准对话框：发现命令后发送 `/codex-fast`。结构化模式会收到 `extension_ui_request`，`method:"input"`、标题 `pi-goui:codex-fast:v1`，`placeholder` 是 `{"level":"normal"}` 的 JSON 字符串（仅当前档位）。v1 档位为 `normal`、`fast`、`ultrafast`；`codex-fast.get` API 还会提供受支持的 `levels` 数组。回复匹配对话 ID 的 `extension_ui_response`，`value` 为普通字符串 `"fast"`，不是再次 JSON 编码的字符串；`cancelled:true` 不保存。未启用结构化模式时使用标准 select/notify。

### 生命周期、版本与进程内替代方案
命令/通知协议使用 v1 标记。未知协议仍保留普通 Pi 对话和通知，只隐藏无法支持的专属控件。后端退出时清理待处理 id，本地取消后忽略迟到回复。适配器在会话启动/关闭、所属会话变化、新设置对话框或显式远程保存后丢弃旧对话框的迟到回答/错误，不再写入或提示；新请求不受影响。停止客户端等待不会撤销已提交保存，因此应重新查询，不猜回滚。

同一 Pi 进程内的 TypeScript 扩展可跳过传输：
~~~ts
import { getCapability } from "/path/to/my-extensions/codex-fast/capabilities.ts";
import { CODEX_FAST_CAPABILITY, CODEX_FAST_CHANGED, type CodexFastService } from "/path/to/my-extensions/codex-fast/api.ts";
const service = getCapability<CodexFastService>(pi, CODEX_FAST_CAPABILITY); // "codex-fast"
if (service) {
  const choices = service.levels;
  const current = service.getConfig(); // {level}，重读持久化状态
  const committed = service.setLevel("fast"); // 权威校验，可能抛错
}
const unsubscribe = pi.events.on(CODEX_FAST_CHANGED, config => {
  // config 为 {level}；更新界面，不在这里写配置。
});
~~~

以上函数/事件只在进程内可用，不是额外远程 RPC 方法。使用时发现服务以容忍加载顺序；缺失就是不可用。适配器销毁时移除监听。

**最小 UI 顺序：**发现 → get → 展示可选项 → set → 关联响应 → 页面激活/保存事实后刷新 → 取消与断连清理。无须知道 codex-fast.json 或后端目录布局。
