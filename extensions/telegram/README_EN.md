# Telegram notifications

[中文](README.md)

Backend-only delivery of primary-session replies and selected `ask_question` notices to a configured Telegram chat. It does not render a local UI, accept Telegram commands, poll for replies, or provide remote control.

## Enable in your own Pi environment

```sh
npm ci --prefix /path/to/extensions
# Supply PI_TG_TOKEN and PI_TG_CHAT securely in the backend environment.
pi --mode rpc --no-extensions -e /path/to/extensions/telegram/index.ts
```

Session-policy support is included in this directory; no sibling `shared/` is needed. A standalone copy still needs the external npm dependency `node-telegram-bot-api` (install it in this directory or its parent); the Pi host supplies its own SDK. Both environment variables are required to send messages. Worker subprocesses are excluded by session policy, not UI availability. Telegram receives actual user/assistant text; enable it only for a chat you control and with permission to transmit that content. Do not embed tokens in frontend source, logs, RPC payloads or examples. Delivery errors are contained and are not proof that a message was delivered.

## Third-party UI integration

**No special integration is required or currently exposed.** This extension registers no UI command, capability lookup API, `pi-ui-rpc` method, dedicated tool details, or frontend fact event. It does not require `pi-ui` or `PI_UI_PROTOCOL`.

1. Run Pi normally, with or without this extension.
2. Render standard Pi `message_start` / `message_update` / `message_end` and tool events as usual.
3. If you support `ask_question`, render and reply to its ordinary or structured Pi dialogs; Telegram delivery is independent and cannot answer the questionnaire.
4. Do not show a delivered/read receipt inferred from `agent_end`. No such acknowledgement protocol is exposed.
5. If the extension, credentials or Telegram network is unavailable, normal Pi UI remains usable; no special retry/control UI is required.

To send user input or cancel Pi work, use standard Pi RPC. There is no Telegram-specific response/cancellation payload and no published delivery-history endpoint. Do not try to discover one by reading private token files or inventing slash commands. Any future public delivery-status interface would need an explicit versioned contract first.
