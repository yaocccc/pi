# 可选 Pi 扩展包

[English](README_EN.md)

这些扩展可安装到任何用户自己的 Pi 环境，无须将本仓库变成 `~/.pi/agent`，也不依赖 Go 前端。扩展具有本机权限，请先审查源码。**不要复制仓库根目录的 settings、models、auth 或其他个人配置。**

## 安装

需要 Node.js **22.19+**、npm，以及支持当前扩展 API 的 Pi **1.5.x**。本地锁文件用于开发/测试，不能替代全局 Pi 宿主；旧版本可能缺少独立工具渲染 API。

将仓库克隆到任意目录后，在该目录运行：

```bash
npm ci --ignore-scripts --prefix ./extensions
pi install /absolute/path/to/checkout/extensions
```

把示例绝对路径替换为自己的克隆路径。第二条命令只将本地包登记到 Pi 设置，并非复制或替换整套个人配置；保留克隆目录以便后续加载。安装前可备份自己的设置。无需发布到 npm，也没有安装钩子。已在自动发现的 agent/extensions 目录中使用本源码时，**不要再重复登记同一扩展**。

想先临时试用、不登记包：

```bash
pi -e /absolute/path/to/checkout/extensions/ask-question/index.ts \
   -e /absolute/path/to/checkout/extensions/pi-ui/index.ts
```

Pi 自身仍需要用户配置模型和认证（例如在原生 Pi 中 `/login`）。不要使用作者的凭据或复制个人模型 ID。

包清单默认加载下表所有入口，并提供 Worker 编排 Skill。按需安装可在 Pi 设置中使用资源过滤：

```json
{
  "packages": [{
    "source": "/absolute/path/to/checkout/extensions",
    "extensions": ["ask-question/index.ts", "pi-ui/index.ts"],
    "skills": []
  }]
}
```

将这个条目**合并**到自己的设置，不要覆盖整个文件。也可用 `pi config` 选择资源。也可以只复制能力目录及对应 `pi-ui/<模块>/` 目录，目标布局为 `<目标>/<模块>/` 与 `<目标>/ui/<模块>/`，无需 `shared/` 或整个源码树。保留两个目录内的全部文件，不能只复制 `index.ts`。各模块 README 给出可直接执行的命令及手动原生 UI 注册示例。

## 仅复制模块安装

对 `ask-question`、`worker`、`codex-fast`、`sol-pi`、`autoname`、`filter-output`，只需把 `extensions/<模块>/` 与 `extensions/pi-ui/<模块>/` 复制为 `<目标>/<模块>/` 和 `<目标>/ui/<模块>/`。加载两个 `index.ts`，或加载能力并在自己的扩展 factory 中调用 UI 的 default 注册函数；不需要、也不要再重复加载整包 `pi-ui/index.ts`。

Pi 1.5.2 提供这些模块所需的 SDK/TUI/TypeBox，无需仓库 `node_modules`、`shared/`、代码生成步骤或作者配置。Worker 路由及其他能力设置仍须使用者自行配置。Telegram 没有 UI 模块，另需自己的外部 npm 依赖。完整复制命令见下方各模块独立指南。

仅维护者：`npm --prefix extensions run sync:portable` 同步已提交源码形式的薄 helper；`node extensions/scripts/sync-portable.mjs --check` 检查漂移。业务实现不复制到 UI，事实类型与显示文案各归所属模块；使用者无需执行这些脚本。

## 功能与配置

| 入口 | 功能与使用条件 |
| --- | --- |
| [ask-question](ask-question/README.md) | 问卷队列、取消、答案校验；交互需要 pi-ui 或其他交互端口，无适配器返回 unavailable 和未回答问题 |
| [worker](worker/README.md) | 独立子任务；须配置自己的三档模型/Thinking，路径限制不是沙箱 |
| [codex-fast](codex-fast/README.md) | Codex 请求档位；缺省 normal，仅对支持的 provider 生效 |
| [pi-ui](pi-ui/README.md) | 可选原生 UI 与 RPC 兼容桥；提供设置命令，不替代能力实现 |
| [sol-pi](sol-pi/README.md) | 大结果归档、在线压缩；压缩模型和经济参数见其文档 |
| autoname | 自动命名；可通过 agent 目录的 autoname.json 配置/关闭，可能调用模型 |
| filter-output | 常见敏感文本过滤；不是完整脱敏或安全保障 |
| telegram | 可选消息集成；仅在 PI_TG_TOKEN 和 PI_TG_CHAT 配置后发送消息 |

默认启用不等于零配置。Worker 配置失败应修复自己的 `worker-settings.json`，不要复制作者配置。Codex 档位、Worker 设置与归档均写入**当前用户的 Pi agent 目录**（缺省 `~/.pi/agent`，可用 `PI_CODING_AGENT_DIR` 指定）。切换用户或目录时，模型、认证和设置不会随源码迁移。

能力与 UI 的接口说明见 [边界文档](shared/FRONTENDS.md)。Go 前端将这些能力视作可选第三方扩展；扩展协议与持久化不由 Go 前端实现。不使用 Go 前端也可运行本包。

## 第三方 UI：按单扩展 README 接入

以下每份指南都独立说明启动/发现、协议示例、生命周期与错误、缺失降级。共享架构文档仅供背景阅读，不是完成最小 UI 的前提；进程内方法与跨进程 Pi RPC 已明确区分。

| 扩展 | 中文 | English | UI 接口 |
| --- | --- | --- | --- |
| ask-question | [指南](ask-question/README.md) | [Guide](ask-question/README_EN.md) | 标准对话框或一次完整结构化问卷 |
| worker | [指南](worker/README.md) | [Guide](worker/README_EN.md) | 工具 details、设置对话框、可选记录 RPC |
| codex-fast | [指南](codex-fast/README.md) | [Guide](codex-fast/README_EN.md) | 状态读写 RPC 或设置对话框 |
| sol-pi | [指南](sol-pi/README.md) | [Guide](sol-pi/README_EN.md) | 只读计划、压缩进度和归档展示 |
| autoname | [指南](autoname/README.md) | [Guide](autoname/README_EN.md) | 改名事实及标准会话元数据 |
| filter-output | [指南](filter-output/README.md) | [Guide](filter-output/README_EN.md) | 过滤事实及标准工具结果 |
| telegram | [指南](telegram/README.md) | [Guide](telegram/README_EN.md) | 无专项 UI API；仅后端发送 |
| pi-ui | [指南](pi-ui/README.md) | [Guide](pi-ui/README_EN.md) | 可选对话框、事实事件和 RPC 传输适配 |

任何 RPC 前端都可用 `PI_UI_PROTOCOL=1` 选择结构化交互，兼容旧 Go 标记和 wire 名称。它不会新增未公开的方法或绕过能力校验。

## 更新与卸载

更新自己克隆的源码后重新执行 `npm ci --ignore-scripts --prefix ./extensions`。原生 Pi 可 `/reload`；Go 前端需重启其 Pi 后端。勿同时加载顶层和内部入口。

`pi remove /absolute/path/to/checkout/extensions` 取消包登记，但不会删除用户配置、会话或归档；临时 `-e` 加载只需停止使用该参数。自动发现方式需从发现目录移走入口，不能靠取消包登记卸载。

## 验证与已知限制

```bash
npm --prefix extensions run test:package
npm --prefix extensions run typecheck
npm --prefix extensions test
```

`npm test` 从仓库根目录运行所有 TypeScript 测试，使用临时 HOME / agent 目录并清除前端环境标记，不使用真实用户凭据。它**不包含** SoL-Pi 的 .mjs 套件。无需为了测试把仓库复制进个人 agent 目录。

SoL-Pi 旧隔离验证器固定 Pi **1.0.4**；其完整套件在当前 1.5.x 宿主下有已复现的基线失败，不能据 TypeScript 测试结果宣称全仓通过。详见 [SoL-Pi 验证限制](sol-pi/README.md#验证限制)。真实终端交互、平台差异和其他宿主版本仍需独立验证。
