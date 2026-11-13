# pi-goui

[English](README_EN.md)

独立的 Go / Bubble Tea 前端，通过标准 Pi RPC 复用 Pi 的模型、工具、认证和扩展。可复制整个 `pi-goui/` 构建，也可只分发二进制；运行不依赖本仓库的其他目录或作者配置。

## 构建与启动

构建需要 Go 1.26+；运行需要已安装并完成认证的 Pi / Node.js。在本目录执行：

```sh
go build -trimpath -o bin/pi-goui ./cmd/pi-goui
./bin/pi-goui --cwd /path/to/project
./bin/pi-goui --check
```

也可用 `./run.sh` 构建并启动。常用选项：

| 选项 | 用途 |
| --- | --- |
| `--pi /path/to/pi` / `PI_BIN` | 指定 Pi 可执行文件，默认从 PATH 查找 |
| `PI_CODING_AGENT_DIR` | 指定 agent 目录，默认 `~/.pi/agent` |
| `--continue` / `-c`、`--session file.jsonl` | 恢复当前目录最近会话，或打开指定会话 |
| `--no-session` | 不持久化会话，不能与恢复选项同时使用 |
| `--check` | 检查 RPC 启动，不发送提示词或保存会话 |
| `-- --model provider/model --no-extensions` | 向 Pi 转发选项；不允许覆盖 RPC 模式、会话归属或传入位置提示词 |

在原生 `pi` 中完成登录，不复制他人的认证或设置。启动前端不会自动安装扩展或调用模型；**提交提示词仍使用 Pi 的正常权限并可能产生费用，本程序不是沙箱。**

## 使用与配置

每个打开的会话有独立 RPC 进程；切换标签保留草稿、队列、对话框及运行任务。再次打开同一会话只切换标签，不创建第二个写入进程。不要让其他 Pi 进程同时修改同一会话文件。

| 命令 | 用途 |
| --- | --- |
| `/new`、`/resume`、`/sessions` | 新建或浏览会话 |
| `/model`、`/thinking` | 选择模型或思考强度 |
| `/fork`、`/origin`、`/tree` | 分叉、查看来源或只读浏览会话树 |
| `/attach` | 附加图片 |
| `/theme`、`/reload` | 选择前端主题、重载前端配置 |
| `/hotkeys` | 查看当前有效快捷键及适用条件 |
| `/close`、`/delete`、`/quit` | 关闭当前会话、确认后永久删除、退出全部会话 |

- 默认 `Enter` 提交、`Shift+Enter` / `Ctrl+J` 换行；`Esc` 中止任务；`Ctrl+N` 新建会话，`Ctrl+↑/↓` 切换标签，`Ctrl+L` 选模型，`Ctrl+T` 切换思考展示，`Ctrl+End` 回到最新。
- `Ctrl+C` 清空输入，空输入时双击退出；`Ctrl+D` 在空输入时关闭当前会话，运行中先确认。关闭最后一个会话会退出前端。
- 点击工具展开预览，使用“查看 ↗”打开完整详情；详情可独立滚动、选择复制和调宽，`Esc` 关闭。预览截断不改变保存记录；估算 token 不是计费 usage。
- 会话选择器中 `Ctrl+R` 重命名、`Ctrl+D` 删除并确认。悬停用户消息标题，点击“从此轮fork”直接新建独立会话，保留原标签与任务；`/tree` 不提供树编辑。
- 读取 agent 目录的 `keybindings.json`、`settings.json` 及项目 `.pi/settings.json`；键位覆盖默认值，空数组禁用动作。增强组合键取决于终端支持。
- `/theme` 预览主题，Enter 保存、Esc 恢复；主题偏好写入 agent 目录的 `goui.json`，不改原生 Pi 主题设置。缺少主题文件时使用内嵌默认值；`/reload` 不重载后端扩展。

本前端不承诺原生 TUI 功能完全对等：认证、完整模型范围管理及原生设置编辑请使用 `pi`。

## 可选扩展

未安装任何第三方扩展时，普通 Pi 工具、codemode 和通用结果展示仍可用。已有适配支持问卷、Worker、Codex 档位及 SoL-Pi/Web 结果；安装方式以各扩展自身文档为准，Go 不直接加载 TypeScript。

Worker 完整记录通过后端能力只读获取，关闭查看器不会停止任务；不可用时显示已有任务结果，已有部分记录会保留并标记不完整。Codex 档位控件仅在桥与能力可用时显示。接入说明见[扩展指南](internal/extensions/README.md)。

## 启动更新通知与 Pi 变更内容

启动后异步只读检查所选 Pi 和全局配置声明的非固定版本 npm/Git 包；不安装或升级，不调用模型，不写入 Pi 设置。无更新、网络失败或安装不兼容时保持安静，没有提示不保证全部最新。

任何非空 `PI_OFFLINE` 或转发的 `-- --offline` 跳过联网检查；本地 Pi 变更内容仍可显示，已展示版本只记录在前端独立缓存中。`--check` 仅检查 RPC，`run.sh` 只构建本地前端。

## 代码结构

- [cmd/pi-goui/](cmd/pi-goui/)：CLI 与启动。
- [internal/core/](internal/core/)：通用配置、RPC、会话、输入、渲染及 UI；`extension/` 定义接入契约，`extensionapi/` 提供低层投影与绘制接口。
- [internal/extensions/](internal/extensions/)：第三方协议、状态、UI 和渲染；集中返回接口列表，由 core 组合后使用。

`internal/` 顶层只保留 `core/` 和 `extensions/`。设计与扩展契约见[扩展指南](internal/extensions/README.md)，修改约束见 [AGENTS.md](AGENTS.md)。

## 验证

测试命令及隔离要求见 [AGENTS.md](AGENTS.md#验证)。默认使用假 RPC；可选真实 RPC 检查必须经只读白名单代理，不允许模型或工具调用。测试结果不等同于真实终端或跨平台验收。

## 来源

基础结构及 RPC 生命周期参考 [cavaldos/pitago](https://github.com/cavaldos/pitago)。保留其 MIT 声明，见 [LICENSE](LICENSE)。
