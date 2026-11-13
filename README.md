# Pi Coding Agent 配置

[English](README_EN.md)

个人 [Pi Coding Agent](https://pi.dev) 配置、可选扩展和独立 Go 终端前端，不是通用发行版。可复用组件不要求替换你的 `~/.pi/agent`，也不需要作者的模型、认证或根目录配置。

## 演示

https://github.com/user-attachments/assets/37766643-9320-4af4-a068-c3702b6a8467

[原始 MP4](pi-coding-agent-demo.mp4)

## 开始使用

- **只用 Go 前端**：已有 Pi、Go 1.26+ 后运行下列命令；首次认证在原生 Pi 中执行 `/login`。前端使用当前用户的 Pi 环境，未安装本仓库扩展也可运行。
- **安装可选能力**：按[扩展指南](extensions/README.md)安装整包，或按模块 README 复制能力与对应 UI 两个目录。已被 Pi 自动发现的源码不要再次登记。
- **保留自己的设置**：不要复制本仓库的 settings、models 或认证配置；扩展拥有本机权限，运行前审查源码。

```sh
git clone <repository-url> /path/to/pi-config
/path/to/pi-config/pi-goui/run.sh --cwd /path/to/project
```

构建、平台依赖、快捷键、配置、更新通知和验证见 [pi-goui 使用说明](pi-goui/README.md)。`pi-goui/` 可单独复制，构建后的二进制也可独立运行。原生 Pi 的 `/reload` 重载扩展；Go 前端的同名命令只重读前端偏好，扩展变更需重启后端。

## 文档入口

| 需要 | 文档 |
| --- | --- |
| 安装、选择或维护扩展 | [扩展总览与模块索引](extensions/README.md) |
| 问卷、Worker、Codex Fast、SoL-Pi 等具体用法 | 扩展总览中的各模块 README；配置、协议和安全限制由对应模块说明 |
| 开发第三方 UI | [能力边界与 RPC 桥](extensions/pi-ui/README.md)及各模块协议 |
| 修改 Go 前端 | [开发约束](pi-goui/AGENTS.md)、[扩展接口](pi-goui/internal/extensions/README.md) |
| 了解 Agent 工作规则 | [工程约束](AGENTS.md)、[Worker 编排技能](extensions/worker/SKILL.md)、[设计问答技能](skills/grill-with-docs/SKILL.md) |

根目录的 `settings.json`、`keybindings.json` 及模块配置只是个人示例；默认值和可用选项以对应指南及源码为准。工具名不等于斜杠命令，未发现的命令不要作为模型提示发送。

## 数据与安全

- 凭据、模型密钥、会话和记忆仅留在忽略的本地文件中。自定义模型建议通过环境变量引用密钥，见 [Pi 模型配置](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/models.md)。
- `.gitignore` 不保护已跟踪文件；发布前检查 `git status --short --ignored` 与 `git diff --cached`。若已泄露，清除公开内容并轮换密钥。
- Worker 路径检查不是 shell/文件系统沙箱。主 Agent 必须审核实际 diff、已有改动和验证证据，不能仅信任结果摘要。
- Telegram 可向目标聊天发送任务内容；自动命名和在线压缩会向模型发送上下文并可能额外计费。启用前确认接收方。
- 工具结果过滤只是启发式防护；Worker 记录与 SoL-Pi 归档可能含原始敏感数据，不等于已脱敏或加密，保留/清理规则见各模块指南。
