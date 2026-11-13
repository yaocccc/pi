# 问卷能力

[English](README_EN.md) · [安装](../README.md)

## 只复制本模块与对应 UI

仅需复制以下**两个完整目录**并保持相对布局，不需要 `shared/`、整个仓库、顶层 `pi-ui/index.ts` 或构建步骤；必要的薄协议辅助代码已随目录附带。

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/ask-question" "$DEST/ui/ask-question"
cp -R "$SRC/ask-question/." "$DEST/ask-question/"
cp -R "$SRC/pi-ui/ask-question/." "$DEST/ui/ask-question/"
~~~

**原生 Pi：直接加载两个入口：**

~~~sh
pi --no-extensions -e "$DEST/ask-question/index.ts" -e "$DEST/ui/ask-question/index.ts"
~~~

**也可由使用者自行注册原生 UI**，例如在 `$DEST/my-ui.ts` 中：

~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/ask-question/index.ts";
export default function (pi: ExtensionAPI) {
  registerUI(pi); // 不注册或替换能力本身
}
~~~

此时加载能力入口与 `my-ui.ts`，不要再加载上述 UI 入口或整包适配器；若能力已自动发现，只补 UI 注册。能力缺少 UI 仍负责执行/校验，不支持的交互应返回不可用，不能默认同意。

RPC 在上述入口命令增加 `--mode rpc`；见下方 wire 示例及[通用 RPC 契约](../pi-ui/README.md)。

Pi 1.5.2 提供 SDK、`pi-tui`、`typebox`，这两个目录运行不需要复制本仓库的 `node_modules`。认证、模型与能力配置仍由使用者自行设置，复制源码不代表零配置。保持上述目录名和相对层级，改变布局须自行调整导入。

加载 `ask-question/index.ts` 提供 `ask_question` 工具；交互需同时加载上方对应 UI 入口，或注册自己的交互端口。不需要独立配置文件。

能力层拥有输入规范化、FIFO、pending、取消、会话关闭与所有前端答案的权威校验，不依赖原生 UI。没有可用适配器时返回未回答问题和 `unavailable: true`，不是用户取消，更不是默认同意。

## 工具用法

调用 `ask_question`：单题用 `question` + `options`，多题用 `questions`（1–8 题）。每题提供 1–8 个选项、可选 `multiSelect`（默认 false）；`questions` 内可加简短 `label`。UI 自动提供自定义输入。

~~~json
{"questions":[{"question":"Choose a target","options":["A","B"],"label":"Target","multiSelect":false}]}
~~~

## 接口

通过 `./capabilities.ts` 的 `getCapability(pi, ASK_QUESTION_SERVICE)` 发现 `ask-question:v1`；接口定义在 `api.ts`。

- `ask({ questions, review, context, signal })` 返回已校验答案；undefined 表示没有适配器，取消返回 `{cancelled:true, answers:[]}`。
- `pending()` 统计正在处理与排队的请求。
- `ask-question:pending` 仅发布数量，不广播答案；等待不会延长任务期限。
- 前端注册 `ask-question.interaction:v1`，实现 `supports(context)` 与 `ask(request)`；收到规范化问题的副本和取消信号。
- 提交答案必须声明完成状态、选项与自定义输入来源；能力层重新验证，不信任 UI。

适配器负责关闭自身界面。失效或不响应取消的适配器不能无限占用能力 pending 状态。用户草稿不等于授权。Go/通用 RPC 和原生 UI 的具体实现都在 pi-ui；保留本模块与对应 UI 两个目录内的全部文件，而不是单独复制入口文件。

## 第三方 UI 快速接入（独立指南）

### 启动与发现
要求 Node 22.19+、Pi 1.5.2。RPC 默认输出结构化消息，无需协议环境变量；`pi-goui:*` 名称不要求 Go 前端。 请实现下述问卷协议，仅支持标准对话框的客户端需要升级。

这是**工具执行时触发的交互**，不是 /ask_question 命令。可以发送 get_commands 发现适配器命令，但其结果不包含工具，不能据此断言 ask_question 是否存在。工具实际执行时处理标准 extension_ui_request；否则保留普通工具结果显示。当前没有远程 ask-question.ask RPC 方法。

### RPC 协议要求
RPC 不再提供通过环境变量选择标准 select/input 问卷的降级路径。请实现下述结构化问卷协议，一次提交完整答案；不要将协议 JSON 当作普通输入框提示显示。

### 一次完整结构化问卷
RPC 默认会收到：
~~~json
{
  "type": "extension_ui_request",
  "id": "q1",
  "method": "input",
  "title": "pi-goui:ask-question:v1",
  "placeholder": "{\"questions\":[{\"question\":\"Choose a target\",\"label\":\"Target\",\"options\":[\"A\",\"B\"],\"multiSelect\":false}],\"review\":true}"
}
~~~

将 placeholder 再解析为 JSON，按顺序展示 questions，遵守 review：未完成页面或草稿不能当成最终同意。questions 有 1–8 项；options 已规范化（清理空白、空项和重复项，最多 8 项）。将**完整提交对象 JSON 编码为 value 字符串**，不要直接把对象放进 value：
~~~json
{
  "type": "extension_ui_response",
  "id": "q1",
  "value": "{\"cancelled\":false,\"answers\":[{\"completed\":true,\"answers\":[\"B\"],\"wasCustom\":false}]}"
}
~~~

每个答案必须有 completed:true 和非空、无重复的 answers 数组。单选恰好一个值，多选至少一个。所有不属于 options 的值都必须在 customAnswers 明确声明。wasCustom 表示所选答案全部来自自定义输入，不是“曾经输入过自定义文本”。以下是含选项 A 的多选题提交示例：
~~~json
{
  "cancelled": false,
  "answers": [
    {
      "completed": true,
      "answers": [
        "A",
        "my target"
      ],
      "wasCustom": false,
      "customAnswers": [
        "my target"
      ]
    }
  ]
}
~~~

协议没有 customInput 字段。前端文本编辑器内容需要转换成 answers、customAnswers，并一致地设置 wasCustom。能力会独立检查选项归属、数量、自定义来源和完成状态；畸形或不匹配的提交变成工具错误（包括 "Invalid questionnaire response"），不会被默认为同意。

取消当前请求：
~~~json
{
  "type": "extension_ui_response",
  "id": "q1",
  "cancelled": true
}
~~~

也可以在 value 内编码 {"cancelled":true,"answers":[]}。不要把部分已完成的题目当成成功提交。能力拥有 FIFO、pending 计数，并在会话关闭时取消待处理工作。远程 UI 应在后端退出/会话替换时丢弃旧请求 id，遵守请求中提供的超时，不能把旧答案重放给另一个会话。标准 Pi 对话协议不保证每次 abort 都会额外发送远程“关闭对话框”事件。

### 显示权威结果
读取 tool_execution_update.partialResult.details、tool_execution_end.result.details 或历史 toolResult 消息的 details，不解析自然语言结果。多题结果示例：
~~~json
{
  "questions": [
    {
      "question": "Choose a target",
      "label": "Target",
      "options": [
        "A",
        "B"
      ],
      "answer": "B",
      "multiSelect": false,
      "wasCustom": false
    }
  ]
}
~~~

兼容单题形状为 {question,options,answer,multiSelect,wasCustom?,customAnswers?}；单选 answer 为字符串，多选为数组，未回答为 null。多题 details 含 questions 及可选 cancelled/unavailable。unavailable:true 是没有适配器，cancelled:true 是取消，二者都不是批准。未知或缺失 details 时保留普通工具输出。

### 改为进程内适配器
只有运行在**同一个 Pi 进程**内的 TypeScript 扩展能使用以下端口。替换时不要同时加载 pi-ui 的问卷适配器。导入路径指向已安装包：
~~~ts
import { registerCapability, getCapability } from "/path/to/my-extensions/ask-question/capabilities.ts";
import { ASK_QUESTION_INTERACTION, ASK_QUESTION_SERVICE, type QuestionInteractionPort, type AskQuestionService } from "/path/to/my-extensions/ask-question/api.ts";
// 在你的 default extension factory(pi) 中：
registerCapability<QuestionInteractionPort>(pi, ASK_QUESTION_INTERACTION, {
  supports: (context) => myFrontend.isConnected(context),
  ask: ({ questions, review, context, signal }) =>
    myFrontend.showForm({ questions, review, context, signal }),
});
const service = getCapability<AskQuestionService>(pi, ASK_QUESTION_SERVICE);
~~~

myFrontend 是你自己的 UI，不是 Pi API；showForm 必须返回上述提交形状，并在 signal 中止时关闭界面。service 提供 ask({questions,review,context,signal}) 和 pending()；没有兼容端口时 ask 返回 undefined。通过 pi.events.on("ask-question:pending", ({pending}) => …) 可订阅进程内计数；这不是答案广播，也不会自动导出到 RPC。

`pi-goui:ask-question:v1` 的后缀是协议版本。未知结构不要猜测或伪造答案；保留普通工具结果显示，将不支持的交互标记为不可用。
