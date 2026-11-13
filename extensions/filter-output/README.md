# 工具结果敏感信息过滤

[English](README_EN.md)

过滤成功工具结果中的部分敏感文本，并整体隐藏指定敏感文件的 read 结果。不改变工具执行、输入、磁盘文件或失败结果，也不是完整数据防泄漏系统。

## 安装

复制两个完整目录并保持相对布局；已包含 helper，无需 shared、node_modules、构建或作者的配置。Pi 提供 SDK 依赖。

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extension
mkdir -p "$DEST/filter-output" "$DEST/ui/filter-output"
cp -R "$SRC/filter-output/." "$DEST/filter-output/"
cp -R "$SRC/pi-ui/filter-output/." "$DEST/ui/filter-output/"
pi --no-extensions -e "$DEST/filter-output/index.ts" -e "$DEST/ui/filter-output/index.ts"
~~~

也可由自己的入口注册（此时不要再单独加载 UI 入口）：
~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/filter-output/index.ts";

export default function (pi: ExtensionAPI) {
  registerUI(pi);
}
~~~

过滤与 UI 无关，仅通知需要适配器。无配置文件、专用工具、斜杠命令或远程方法。

## 过滤范围与局限

- read 命中 .env/.env.*、.dev.vars*、secret(s).json/yaml/yml/toml 或 credentials 前缀文件名时，整个 content 替换为安全占位文本并发布 filter.redacted 事实；不保留原行结构。读取 .env.example 直接跳过处理，并非所有 example 后缀都豁免。
- 其他成功结果（包括普通 read、bash 及其他工具）的 text block 使用启发式规则，覆盖部分密钥前缀、token/password 赋值、数据库凭据、私钥等。非 text block 原样保留；只有实际替换才返回新结果，此路径不发布专用事实。
- 不保证覆盖未知格式、图片或错误消息，也可能误判普通文本。原始数据可能仍在磁盘、归档或其他日志中；不能替代授权、访问控制、密钥轮换或人工审查。

权威实现见 [index.ts](index.ts)；前端不重复执行脱敏，也不尝试还原文本。

## 第三方 UI：只消费处理后结果

使用标准 Pi 工具事件/消息中的结果作为显示源，无需调用过滤扩展。无适配器或通知缺失不代表过滤关闭。

加载对应 UI 后，敏感文件 read 被整体隐藏时会收到（其他文本掩码没有该事件）：
~~~json
{"type":"extension_ui_request","id":"notice-filter-1","method":"notify","notifyType":"warning","message":"pi-goui:event:v1:{\"name\":\"filter.redacted\",\"data\":{\"path\":\".env\"}}"}
~~~

只剥离精确 pi-goui:event:v1: 前缀并解析 JSON。data.path 是被隐藏的文件路径，展示时应考虑路径隐私；事实不包含文件内容或恢复句柄。无需 extension_ui_response，未知/畸形事件忽略，不将历史通知重放为新警告。

同进程宿主可订阅 pi:frontend-event:v1 并按 event.name="filter.redacted" 过滤 {context,event}；context 用于来源归属，卸载时取消订阅。观察者异常不影响过滤结果。不提供启停、规则管理、导出原文或 show-original 的 UI 协议。

通用边界见 [pi-ui](../pi-ui/README.md)。此事件只表示隐藏了某个敏感文件结果，不能作为所有结果均已完整脱敏的证明。
