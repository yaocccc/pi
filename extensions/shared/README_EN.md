# Shared capability contracts

[中文](README.md) · [Installation](../README_EN.md)

This directory has no extension entry and contains no native drawing or interaction.

- `capabilities.ts`: bus-local service and interaction-port registration/discovery.
- `frontend-events.ts`: typed business facts.
- `frontend.ts`: optional fact publication, without frontend identity or notification formatting.
- `token-estimate.ts`: pure token estimation, without colors.
- `session-policy.ts`: explicit primary/Worker session policy; background integrations do not infer execution policy from `hasUI`.

Presentation lives in each pi-ui module; pi-ui/shared provides maintenance templates and whole-package compatibility. Capabilities must not depend on pi-ui. See [boundaries](FRONTENDS_EN.md). This directory is **not** needed by copy-only installations: the module/UI pair already includes its thin support files. Capability-owned event types are aggregated here for compatibility.
