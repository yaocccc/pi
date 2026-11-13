# Telegram 通知

[English](README_EN.md)

仅在后端把主会话回复及部分 `ask_question` 通知发送到指定 Telegram 聊天。不绘制本地界面，不接收 Telegram 指令，不轮询回复，也不提供远程控制。

## 在自己的 Pi 环境启用

```sh
npm ci --prefix /path/to/extensions
# 在后端环境中安全配置 PI_TG_TOKEN 和 PI_TG_CHAT。
pi --mode rpc --no-extensions -e /path/to/extensions/telegram/index.ts
```

会话策略代码已包含在本目录，无需相邻 `shared/`。单独复制时仍需在本目录或父目录安装外部 npm 依赖 `node-telegram-bot-api`；Pi 宿主提供其 SDK。发送需要两个环境变量均已配置。通过明确会话策略排除 Worker 子进程，不以是否有 UI 判断。Telegram 会收到真实用户/助手文本，请仅对有权限传输的内容和自己控制的聊天启用。不要把 token 放进前端源码、日志、RPC 数据或示例。发送错误会被隔离，不能据此推断消息已送达。

## 第三方 UI 如何接入

**目前无需、也没有专项接入接口。** 本扩展没有注册 UI 命令、能力查询 API、`pi-ui-rpc` 方法、专用工具 details 或前端事实事件，不依赖 `pi-ui` / `PI_UI_PROTOCOL`。

1. 正常运行 Pi，装不装本扩展都一样。
2. 按标准 Pi `message_start` / `message_update` / `message_end` 及工具事件展示对话。
3. 支持 `ask_question` 时，正常渲染并回复其标准或结构化对话框；Telegram 发送独立进行，不能替用户回答。
4. 不要从 `agent_end` 推断“已送达/已读”：目前没有此确认协议。
5. 扩展、凭据或网络不可用时，普通 Pi UI 仍可使用，无需专用重试/控制界面。

输入与取消 Pi 工作仍使用标准 Pi RPC，没有 Telegram 专用响应/取消格式，也没有公开的发送历史端点。不要读取私有 token 文件或编造斜杠命令来探测。若未来需要投递状态接口，应先定义明确的版本化契约。
