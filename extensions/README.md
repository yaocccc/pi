# Pi 可选扩展

[English](README_EN.md) · [仓库入口](../README.md)

能力模块负责执行、权威校验、状态与持久化；可选 `pi-ui` 负责原生展示、交互和 RPC 桥。无 UI 时能力仍可加载，不支持的交互返回不可用，不能视为用户同意。开发边界见 [pi-ui](pi-ui/README.md)。

## 安装整包

需要已有 Pi 与自己的模型/认证配置。需要 **Node 22.19+**。完整扩展回归使用仓库 SDK **1.5.0**；SoL-Pi 另以已安装的全局 SDK **1.5.2** 通过差分、会话及严格类型检查。其他 SDK 版本须重新验证。

```sh
SRC=/path/to/repository/extensions
npm ci --prefix "$SRC"
pi install "$SRC"
```

包入口由 [package.json](package.json) 的 `pi.extensions` 声明；安装只登记本地包，不复制作者设置、不升级全局 Pi，也不安装根设置中的其他包。先在原生 Pi 执行 `/login`；Worker 的三个模型档位需自行配置。

若代码已在 agent 的 `extensions/` 中被自动发现，不要再 `pi install` 或显式重复加载。Pi 发现顶层 .ts 和带 index.ts 的目录；内部入口、维护 helper 与测试不能作为额外扩展加载。

按需加载可在自己的 Pi 设置中显式列出入口（不要同时登记整包）：

```json
{
  "extensions": [
    "/path/to/repository/extensions/ask-question/index.ts",
    "/path/to/repository/extensions/worker/index.ts",
    "/path/to/repository/extensions/pi-ui/index.ts"
  ]
}
```

整包 UI 未找到其他能力时安全降级。命令行可用 `pi --no-extensions -e /path/to/capability/index.ts -e /path/to/ui/index.ts` 测试选定入口。

## 只复制单个模块

ask-question、worker、codex-fast、sol-pi、autoname、filter-output 支持双目录分发：

```text
extensions/<模块>/         → <目标>/<模块>/
extensions/pi-ui/<模块>/   → <目标>/ui/<模块>/
```

复制两个**完整目录**并保持布局，加载能力与对应 UI index.ts，或由自己的入口调用 `registerUI(pi)`，不要重复注册。模块已附带 helper；无需 shared、node_modules、构建或生成步骤，SDK 依赖由 Pi 提供。每个能力 README 是该复制单元唯一接入指南，包含准确复制命令、注册与 RPC 示例。

Telegram 不属于上述六组，需要外部依赖，按其指南安装。第三方前端也可不加载原生 UI，但当前结构化 RPC 交互仍需桥接适配器；删除整个 UI 不会自动提供这些协议。

## 第三方 UI：按单扩展 README 接入

| 模块 | 用途与入口 | 指南 |
| --- | --- | --- |
| ask-question | Agent 工具 `ask_question`；排队、取消与答案校验 | [问卷](ask-question/README.md) |
| worker | Agent 工具 `worker`；UI 命令 `/worker_settings` | [调度、设置与只读记录](worker/README.md) |
| codex-fast | UI 命令 `/codex-fast`；后端档位策略与保存 | [Fast / Ultrafast](codex-fast/README.md) |
| sol-pi | `obs_recall`、`update_plan`；归档与在线压缩 | [配置、协议与兼容限制](sol-pi/README.md) |
| autoname | 主会话自动命名；可有额外模型请求 | [命名策略](autoname/README.md) |
| filter-output | 成功工具结果的启发式敏感信息过滤 | [过滤范围与局限](filter-output/README.md) |
| telegram | 环境变量配置的出站通知，无远程控制 | [依赖与数据外传](telegram/README.md) |
| pi-ui | 上述能力的可选宿主适配器，不替换工具执行 | [通用边界、发现与 RPC](pi-ui/README.md) |

各能力指南拥有本模块的用法、配置和 wire 示例，不必逆向 Go 前端。缺能力/桥时禁用专项入口，未知载荷回退通用工具显示；不虚构远程方法，也不直接读取第三方私有文件。

## 更新、卸载与维护

- 更新本地源码后重新 `npm ci --prefix "$SRC"`；原生 Pi 使用 `/reload`，Go/RPC 前端重启后端。Go 的 `/reload` 只重读前端偏好。
- 整包用 `pi remove "$SRC"` 注销；显式入口/复制安装则移除对应设置与 UI 注册。配置、记录和归档不会因此自动清除，按模块说明另行处理。
- [shared/](shared/) 和 [pi-ui/shared/](pi-ui/shared/) 只提供维护模板、类型聚合与兼容 helper，没有独立入口。修改可移植 helper 后运行 `npm --prefix extensions run sync:portable` 并核对 diff；消费者无需运行它。
- 只维护本总览、能力模块和 pi-ui 的双语 README，不再为共享目录或 UI 子目录复制说明。技能与角色提示词是运行资源，不能当重复文档删除。

从仓库根验证：

```sh
npm ci --prefix extensions
npm --prefix extensions run typecheck
npm --prefix extensions test
```

默认入口收集全部 `.test.ts` 与 `.test.mjs`，包括 SoL-Pi 历史套件，覆盖文档 JSON/真实 schema、复制布局、helper 同步、能力边界、RPC 和原生会话差分；仅归档 UI 的既有交互测试跳过。runner 使用临时 HOME/agent、过滤调用者凭据及会话环境，不调用真实模型。全局 SDK 的独立离线入口和版本边界见 [SoL-Pi 验证](sol-pi/README.md#兼容性与验证)。Go/PTY/Web 的完整入口见 [前端验证](../pi-goui/README.md#验证)。这些检查不代替实体终端、其他平台或真实模型验收。
