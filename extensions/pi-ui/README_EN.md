# Optional Pi UI adapter

[中文](README.md) · [Installation](../README_EN.md)

The optional whole-package `index.ts` assembly owns native forms, tool cards, menus, notifications, refresh, and SoL-Pi hidden cards. It does not execute capability tools or persist their configuration.

- Renderers attach via `registerToolRenderer`; tools are never replaced.
- Services are discovered at use time through the session bus, so load order is irrelevant.
- `/worker_settings` and `/codex-fast` belong here; capabilities validate and save.
- Legacy Go/generic RPC dialogs and fact notifications remain supported.
- The optional `pi-ui-rpc` command bridges registered methods using standard Pi RPC, not a new socket/server. Codex get/set and Worker transcript reading delegate to capabilities.
- Without this adapter, capabilities still run. Questionnaires return unavailable unless another interaction port is installed. Go must discover the bridge before invoking it and degrade when a capability is absent.

Choose either this whole-package entry or only the selected module entries/manual registration, never both. Copy-only installs need only the capability directory and its matching UI directory; shared source trees are not required. The former extensions/ui implementation is not required or shipped here. Reload native Pi or restart the Go backend after changes.

See [boundaries](../shared/FRONTENDS_EN.md). From the checkout root:

```bash
npm --prefix extensions run typecheck
npm --prefix extensions test
```

Architecture tests check dependency direction; integration tests use the SDK loader with the adapter absent, first, and last. Passing automated tests is not a substitute for manual frontend interaction checks.

## Copy just this capability and its UI

Copy these **two complete directories**, keeping the relative layout below. No `shared/`, whole repository, top-level `pi-ui/index.ts`, or build step is required. Thin support code is already included.

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/worker" "$DEST/ui/worker"
cp -R "$SRC/worker/." "$DEST/worker/"
cp -R "$SRC/pi-ui/worker/." "$DEST/ui/worker/"
~~~

~~~text
my-extensions/
├── worker/index.ts        # capability, including all its local files
└── ui/worker/index.ts     # optional UI, including all its local files
~~~

**Native Pi, explicit entry points:**

~~~sh
pi --no-extensions -e "$DEST/worker/index.ts" -e "$DEST/ui/worker/index.ts"
~~~

**Or register native UI yourself**, for example in `$DEST/my-ui.ts`:

~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/worker/index.ts";
export default function (pi: ExtensionAPI) {
  registerUI(pi); // does not register/replace the capability
}
~~~

Load the capability and `my-ui.ts` instead of the UI entry above; do not load both registration paths or the whole-package adapter too. With automatic capability discovery, only add the UI registration. Without this UI, the capability still owns execution/validation; unsupported interactions must remain unavailable, never implicit consent.

For RPC, add `PI_UI_PROTOCOL=1` and `--mode rpc` to the explicit-entry command; the detailed wire contract follows below. Worker/Codex UI modules include their own optional method bridge and coordinate to register it once when combined. Other modules do not invent additional methods.

Pi 1.5.2 supplies the SDK, `pi-tui` and `typebox`; you do not need this repository's `node_modules` for these two directories. Use your own authentication, models and capability settings; copying source is not configuration. Keep directory names/layout as shown, or adjust imports yourself. Maintainers sync bundled helpers; consumers **do not** run generation scripts.

The same layout/registration applies to `ask-question`, `worker`, `codex-fast`, `sol-pi`, `autoname`, and `filter-output`. Each UI subdirectory has a bilingual README. Only Worker/Codex register the shared method bridge; independent copies cooperate through the Pi event bus, not global module state.

## Third-party UI transport quick start

Load this adapter with the capability you need; it has no business state by itself:

~~~sh
PI_UI_PROTOCOL=1 pi --mode rpc --no-extensions \
  -e /path/to/extensions/codex-fast/index.ts \
  -e /path/to/extensions/pi-ui/index.ts
~~~

`PI_UI_PROTOCOL=1` opts any RPC UI into structured dialogs and fact notifications. Legacy `PI_GOUI_PROTOCOL=1` is still accepted; historical `pi-goui:...:v1` titles/prefixes remain wire identifiers, not a Go requirement. With neither flag, ordinary select/input/confirm/editor fallback works where supported. The method bridge itself is not gated by these flags.

1. Send `{"id":"discover","type":"get_commands"}`. Require an item with `name:"pi-ui-rpc", source:"extension"` before invoking the bridge; never send an unknown slash command that might reach the model.
2. Send a prompt whose message is `/pi-ui-rpc {"id":"op-1","method":"codex-fast.get","params":{}}`.
3. Handle the ordinary prompt response separately. The actual operation reply arrives as `extension_ui_request`, `method:"notify"`, with `message` starting `pi-ui:rpc:v1:`. Parse the suffix as `{id,result}` or `{id,error:{code,message}}`; match `id:"op-1"`, not the host notification ID.
4. A successful result for this method is `{"level":"normal","levels":["normal","fast","ultrafast"]}`. Missing bridge/capability means unavailable, not permission to read its private files.
5. Keep request ownership per Pi process/tab. Local timeout/cancellation stops waiting; there is no per-operation cancel command and a committed write is not rolled back. Ignore late replies. Unknown methods or versions must safely degrade.

Current methods are only `codex-fast.get`, `codex-fast.set` (`params:{level}`), and `worker.transcript` (`params:{descriptor,cursor?}`). Each capability's standalone README specifies its payload, authority and lifecycle; do not infer additional methods from a name. The bridge limits double-JSON-encoded replies to a 32 MiB message budget; oversized responses return `response-too-large` rather than disconnecting Pi.

For Worker, load `worker/index.ts` instead of (or alongside) Codex Fast. Copy the opaque descriptor from a Worker task snapshot; the UUID below is illustrative. The optional `cursor` must be the previous backend-returned token, not a byte offset:

~~~json
{"id":"read-transport-1","type":"prompt","message":"/pi-ui-rpc {\"id\":\"read-1\",\"method\":\"worker.transcript\",\"params\":{\"descriptor\":{\"version\":1,\"id\":\"11111111-1111-4111-8111-111111111111\"}}}"}
~~~


For dialogs, use the matching `extension_ui_response.id` and the capability's documented answer format; dismiss with `cancelled:true`. Fact notifications use a different prefix, `pi-goui:event:v1:`, with `{name,data}`; no reply is required. The adapter never exposes TypeScript UI components over RPC or performs capability persistence.
