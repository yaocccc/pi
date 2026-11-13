# Codex Fast capability

[中文](README.md) · [Installation](../README_EN.md)

## Copy just this capability and its UI

Copy these **two complete directories**, keeping the relative layout below. No `shared/`, whole repository, top-level `pi-ui/index.ts`, or build step is required. Thin support code is already included.

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/codex-fast" "$DEST/ui/codex-fast"
cp -R "$SRC/codex-fast/." "$DEST/codex-fast/"
cp -R "$SRC/pi-ui/codex-fast/." "$DEST/ui/codex-fast/"
~~~

~~~text
my-extensions/
├── codex-fast/index.ts        # capability, including all its local files
└── ui/codex-fast/index.ts     # optional UI, including all its local files
~~~

**Native Pi, explicit entry points:**

~~~sh
pi --no-extensions -e "$DEST/codex-fast/index.ts" -e "$DEST/ui/codex-fast/index.ts"
~~~

**Or register native UI yourself**, for example in `$DEST/my-ui.ts`:

~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/codex-fast/index.ts";
export default function (pi: ExtensionAPI) {
  registerUI(pi); // does not register/replace the capability
}
~~~

Load the capability and `my-ui.ts` instead of the UI entry above; do not load both registration paths or the whole-package adapter too. With automatic capability discovery, only add the UI registration. Without this UI, the capability still owns execution/validation; unsupported interactions must remain unavailable, never implicit consent.

For RPC, add `PI_UI_PROTOCOL=1` and `--mode rpc` to the explicit-entry command; the detailed wire contract follows below. Worker/Codex UI modules include their own optional method bridge and coordinate to register it once when combined. Other modules do not invent additional methods.

Pi 1.5.2 supplies the SDK, `pi-tui` and `typebox`; you do not need this repository's `node_modules` for these two directories. Use your own authentication, models and capability settings; copying source is not configuration. Keep directory names/layout as shown, or adjust imports yourself. Maintainers sync bundled helpers; consumers **do not** run generation scripts.

`index.ts` loads independently of any UI. It owns configuration validation,
legacy `fast`/`ultrafast` migration, atomic persistence, and the
`before_provider_request` service-tier mapping. It registers no UI commands.

Discover the service through the host's session event bus:

```ts
import { getCapability } from './capabilities.ts';
import { CODEX_FAST_CAPABILITY, type CodexFastService } from './api.ts';

const service = getCapability<CodexFastService>(pi, CODEX_FAST_CAPABILITY);
const state = service?.getConfig();
const supported = service?.levels;
service?.setLevel('fast');
```

- Capability name: `codex-fast`.
- `getConfig()` reloads persisted state and returns a detached `{ level }` snapshot.
- `levels` is an immutable list of supported levels.
- `setLevel(unknown)` validates and atomically saves, returning a detached state.
  Invalid values throw an error with `code: 'invalid-level'`; persistence errors
  propagate without updating the committed in-memory configuration.
- Successful service writes emit `codex-fast:changed` with `{ level }`. This is a
  fact event, not a UI notification. External file replacements are reloaded at
  the next read/provider request; they do not emit this event.

The optional `../pi-ui/codex-fast/index.ts` adapter exports
`registerCodexFastUI(pi)`. It registers `/codex-fast` and owns native menus,
notifications, and the legacy Go RPC dialog protocol. It resolves the capability
when invoked, so either extension installation order works. A future remote UI
can bridge the service methods without reimplementing validation or file writes.

The adapter also exposes `codex-fast.get` / `codex-fast.set` through the optional
`pi-ui-rpc` bridge. Go consumes these methods instead of reading or writing the
configuration. Missing bridges/capabilities disable the optional control.

## Third-party UI quick start (self-contained)

### Start and discover
~~~sh
PI_UI_PROTOCOL=1 pi --mode rpc --no-extensions \
  -e /path/to/my-extensions/codex-fast/index.ts \
  -e /path/to/my-extensions/ui/codex-fast/index.ts
~~~
Use your own Pi credentials/settings. Keep all files inside the two copied directories; do not copy just index.ts. Node 22.19+ and the currently tested Pi 1.5.2 host are the baseline. PI_UI_PROTOCOL=1 opts into structured extension UI; the historical PI_GOUI_PROTOCOL=1 alias and pi-goui:* wire names remain compatible. No Go frontend is required. On older adapters that do not yet recognize PI_UI_PROTOCOL, replace that environment assignment with PI_GOUI_PROTOCOL=1; the wire payloads below are identical.

First send this standard Pi request; match its response by id:

~~~json
{
  "id": "discover",
  "type": "get_commands"
}
~~~

Inspect response.data.commands for the required command with source="extension". A same-named prompt template/skill is not sufficient. If absent, hide the control; **do not send an unknown slash command**, because Pi may treat it as model input. Discovery confirms the adapter command, not every underlying capability.
Require pi-ui-rpc for the data API or codex-fast for the optional settings dialog. If the bridge exists but this capability is not loaded, its method returns error.code="unavailable"; hide the tier control, keep ordinary Pi operation available.

### Read and set through Pi RPC
All examples are individual JSONL records written to Pi stdin; parse stdout as JSONL and route by type/id. No direct socket, file access or TypeScript import is involved:
~~~json
{
  "id": "rpc-1",
  "type": "prompt",
  "message": "/pi-ui-rpc {\"id\":\"tier-get-1\",\"method\":\"codex-fast.get\",\"params\":{}}"
}
~~~

The ordinary response with id rpc-1 acknowledges the prompt (normally data.disposition="handled"); it is **not** the method result. The result is a notify message:
~~~json
{
  "type": "extension_ui_request",
  "id": "notification-1",
  "method": "notify",
  "notifyType": "info",
  "message": "pi-ui:rpc:v1:{\"id\":\"tier-get-1\",\"result\":{\"level\":\"normal\",\"levels\":[\"normal\",\"fast\",\"ultrafast\"]}}"
}
~~~

Strip only the exact pi-ui:rpc:v1: prefix, JSON.parse the remainder, and correlate its inner id tier-get-1. The outer notification id is unrelated. Store level as the current backend-confirmed value and levels as available choices; do not derive availability from provider/model names.

To save:
~~~json
{
  "id": "rpc-2",
  "type": "prompt",
  "message": "/pi-ui-rpc {\"id\":\"tier-set-1\",\"method\":\"codex-fast.set\",\"params\":{\"level\":\"fast\"}}"
}
~~~

The inner result is again {level,levels}. Update display only after success. Core validation, legacy-file migration, atomic persistence and provider request tier selection remain backend responsibilities. An invalid selection returns this inner envelope:
~~~json
{
  "id": "tier-set-1",
  "error": {
    "code": "invalid-level",
    "message": "Invalid Codex level: invalid"
  }
}
~~~

Other errors include unavailable, invalid-request, response-too-large, extension-error or an I/O code such as EACCES. Preserve the last confirmed state on errors; show an error rather than pretending a save succeeded. Unknown errors remain opaque diagnostic messages. The bridge bounds the twice-encoded response below the transport line limit.

A successful save also publishes this fact when the structured adapter is active:
~~~json
{
  "type": "extension_ui_request",
  "id": "notice-2",
  "method": "notify",
  "notifyType": "info",
  "message": "pi-goui:event:v1:{\"name\":\"codex.settings-saved\",\"data\":{\"level\":\"fast\"}}"
}
~~~

Use it to refresh or invalidate the displayed state, not as a substitute for correlating a request reply. This protocol is not a push feed for every external file edit or other backend process: refetch on activation and use bounded polling if live synchronization is required. Per-tab/back-end state must not leak between sessions.

### Optional dialog instead of a custom control
An alternative is the standard dialog path: after discovery, send `prompt` with `/codex-fast`. In structured mode it requests `extension_ui_request` with `method:"input"`, title `pi-goui:codex-fast:v1`, and `placeholder` containing the JSON string `{"level":"normal"}` (current level only). Wire v1 offers `normal`, `fast`, and `ultrafast`; the `codex-fast.get` API additionally returns the supported `levels` array. Reply with `extension_ui_response` whose `id` matches the dialog and whose `value` is the plain string `"fast"` (not JSON-encoded again). Send `cancelled:true` to dismiss without saving. Without structured mode, standard select/notify requests are used.

### Lifecycle, versioning and an in-process alternative
The command prefix and notification suffixes carry v1. Keep standard Pi dialogs/notifications usable for unknown protocols; hide only unsupported specialized controls. Drop pending IDs when the backend exits, and ignore late replies after local cancellation. Cancelling client-side waiting does not undo a save already committed, so refetch rather than guessing rollback.

A TypeScript extension in the same Pi process may bypass transport:
~~~ts
import { getCapability } from "/path/to/my-extensions/codex-fast/capabilities.ts";
import { CODEX_FAST_CAPABILITY, CODEX_FAST_CHANGED, type CodexFastService } from "/path/to/my-extensions/codex-fast/api.ts";
const service = getCapability<CodexFastService>(pi, CODEX_FAST_CAPABILITY); // "codex-fast"
if (service) {
  const choices = service.levels;
  const current = service.getConfig(); // {level}; reloads persistence
  const committed = service.setLevel("fast"); // validates and may throw
}
const unsubscribe = pi.events.on(CODEX_FAST_CHANGED, config => {
  // config is {level}; update your view, never write the file here.
});
~~~

These are process-local functions/events, not additional remote RPC names. Get the service at use time to tolerate load order; absence means unavailable. Remove listeners when your adapter is disposed.

**Minimal UI:** discover → get → display choices → set → correlate reply → refresh on activation/save facts → cancellation/disconnect cleanup. No knowledge of codex-fast.json or backend filesystem layout is required.
