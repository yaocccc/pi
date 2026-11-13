# 共享能力契约

[English](README_EN.md) · [安装](../README.md)

此目录没有扩展入口，不会独立加载，也不包含原生绘制或交互。

- `capabilities.ts`：通过会话事件总线注册/发现进程内能力与交互端口。
- `frontend-events.ts`：结构化业务事实类型。
- `frontend.ts`：向可选观察者发布事实，不判断前端身份或生成通知。
- `token-estimate.ts`：纯 token 估算，不负责颜色与展示。
- `session-policy.ts`：显式区分主会话与 Worker 子会话，后台集成不再根据 `hasUI` 决定是否执行。

原生通知、边框和 token 展示由对应 `pi-ui/<模块>/` 承担；[`../pi-ui/shared/`](../pi-ui/shared/) 保留维护模板与整包兼容聚合。能力不能反向依赖 `pi-ui`。本目录**不是双目录复制安装的依赖**：薄协议/helper 已随各模块附带；业务事实类型由能力自身定义，此处仅聚合。

完整接口、依赖方向与旧 RPC 兼容范围见 [FRONTENDS.md](FRONTENDS.md)。
