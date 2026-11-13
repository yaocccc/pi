# Automatic session naming

[中文](README.md)

## Copy just this capability and its UI

Copy these **two complete directories**, keeping the relative layout below. No `shared/`, whole repository, top-level `pi-ui/index.ts`, or build step is required. Thin support code is already included.

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/autoname" "$DEST/ui/autoname"
cp -R "$SRC/autoname/." "$DEST/autoname/"
cp -R "$SRC/pi-ui/autoname/." "$DEST/ui/autoname/"
~~~

~~~text
my-extensions/
├── autoname/index.ts        # capability, including all its local files
└── ui/autoname/index.ts     # optional UI, including all its local files
~~~

**Native Pi, explicit entry points:**

~~~sh
pi --no-extensions -e "$DEST/autoname/index.ts" -e "$DEST/ui/autoname/index.ts"
~~~

**Or register native UI yourself**, for example in `$DEST/my-ui.ts`:

~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/autoname/index.ts";
export default function (pi: ExtensionAPI) {
  registerUI(pi); // does not register/replace the capability
}
~~~

Load the capability and `my-ui.ts` instead of the UI entry above; do not load both registration paths or the whole-package adapter too. With automatic capability discovery, only add the UI registration. Without this UI, the capability still owns execution/validation; unsupported interactions must remain unavailable, never implicit consent.

For RPC, add `PI_UI_PROTOCOL=1` and `--mode rpc` to the explicit-entry command; the detailed wire contract follows below. Worker/Codex UI modules include their own optional method bridge and coordinate to register it once when combined. Other modules do not invent additional methods.

Pi 1.5.2 supplies the SDK, `pi-tui` and `typebox`; you do not need this repository's `node_modules` for these two directories. Use your own authentication, models and capability settings; copying source is not configuration. Keep directory names/layout as shown, or adjust imports yourself. Maintainers sync bundled helpers; consumers **do not** run generation scripts.

Generates a session name after a primary (non-Worker) session settles. It preserves manual names, owns model selection/cooldown/staleness checks, and does not depend on having a UI. This optional background work can call the user's configured model and incur its normal costs.

## Third-party UI quick start (self-contained)
~~~sh
PI_UI_PROTOCOL=1 pi --mode rpc --no-extensions \
  -e /path/to/my-extensions/autoname/index.ts \
  -e /path/to/my-extensions/ui/autoname/index.ts
~~~
Use your own Pi credentials/settings. Keep all files inside the two copied directories; do not copy just index.ts. Node 22.19+ and the currently tested Pi 1.5.2 host are the baseline. PI_UI_PROTOCOL=1 opts into structured extension UI; the historical PI_GOUI_PROTOCOL=1 alias and pi-goui:* wire names remain compatible. No Go frontend is required. On older adapters that do not yet recognize PI_UI_PROTOCOL, replace that environment assignment with PI_GOUI_PROTOCOL=1; the wire payloads below are identical.

For installation without RPC transport, load only this capability; execution still works, but it emits only process-local facts. The optional pi-ui entry adapts facts to RPC/native presentation. This extension registers **no slash command, tool or remote control method**.

### Discover safely and receive facts
You may send {"id":"commands-1","type":"get_commands"} to discover the backend's commands, but its list cannot prove this passive capability exists. Do not send an invented /autoname or pi-ui-rpc autoname.get request as a probe. Remain a normal Pi UI if no fact arrives; no permanent feature control is needed.

With the structured marker, receive:
~~~json
{
  "type": "extension_ui_request",
  "id": "notice-1",
  "method": "notify",
  "notifyType": "info",
  "message": "pi-goui:event:v1:{\"name\":\"autoname.renamed\",\"data\":{\"name\":\"Inspect API behavior\",\"contextTokens\":4200,\"durationMs\":1250}}"
}
~~~

Parse only the exact pi-goui:event:v1: prefix, then {name,data}. That legacy prefix is used by non-Go UIs too. The outer notifyType is transport metadata, not the extension's authoritative severity. No extension_ui_response is sent for notify.
~~~js
// Attach to your JSONL parser for Pi stdout; msg is one decoded record.
function factFromPi(msg) {
  if (msg.type !== "extension_ui_request" || msg.method !== "notify") return;
  const prefix = "pi-goui:event:v1:";
  if (typeof msg.message !== "string" || !msg.message.startsWith(prefix)) return;
  try {
    const event = JSON.parse(msg.message.slice(prefix.length));
    return event.name === "autoname.renamed" ? event.data : undefined;
  } catch { return undefined; }
}
~~~
## Configuration and data
The user's autoname.json supports enabled, notify, cooldownSeconds, model and reasoning. Defaults are {"enabled":true,"notify":true,"cooldownSeconds":600,"model":"auto","reasoning":"minimal"}; model may be auto or provider/model. The capability reloads its own configuration on settled turns. UI adapters must not open this private configuration to implement a second control path.

name is a string, contextTokens is the input context-token estimate, and durationMs is elapsed milliseconds. The notification is emitted only when configured notify is true and a name was committed. Silence is not evidence of failure or feature absence. The event does not carry an editable draft or a naming request that needs confirmation.

Read the authoritative name with standard Pi RPC:
~~~json
{
  "id": "state-1",
  "type": "get_state"
}
~~~

The standard response data contains sessionName (when set), sessionId and other Pi state. Refresh on session activation and normal session metadata changes; a live fact can prompt another get_state. This also restores names missed while disconnected. Do not infer naming completion from the arrival order of unrelated model events.

For an explicit user rename, use Pi's **standard** set_session_name command, not an invented extension method:
~~~json
{
  "id": "rename-1",
  "type": "set_session_name",
  "name": "My deliberate name"
}
~~~

Only do that on an explicit user action; do not automatically send it back for every autoname fact. The normal response reports success/error. The naming capability will retain a manual name rather than overwriting it.

## Same-process adapter (without pi-ui)
Only an extension inside the same Pi process can subscribe to this bus:
~~~ts
import { FRONTEND_EVENT, type FrontendEventMessage } from "/path/to/my-extensions/autoname/frontend.ts";
const unsubscribe = pi.events.on(FRONTEND_EVENT, (raw: unknown) => {
  const { context, event } = raw as FrontendEventMessage;
  if (event.name === "autoname.renamed") {
    // Render event.data for context.sessionManager.getSessionId().
  }
});
~~~
Here the payload is {context,event}; remote clients receive only {name,data}. There is no getCapability service to invoke. Remove the listener on adapter disposal. UI observer failures must not affect the capability's outcome.

## Lifecycle, errors and minimal implementation
Route events to the source backend/session; clear stale toasts on session replacement and ignore unknown event names/versions or malformed payloads without showing raw protocol JSON. No stream of past notifications is replayed automatically. A lost connection or closed toast cannot roll back work already done by the capability. Do not fabricate a “cancel operation” API.

Keep ordinary RPC notify/tool/session behavior for missing adapters. Under generic RPC without the structured marker, this adapter does not export these machine-readable facts; use standard Pi state/results instead. Native Pi may receive a formatted notice.

**Minimal implementation:** standard Pi UI → optional fact parser → type-check expected data → display/refresh → dispose session-owned observers. No filesystem access, credentials, backend imports, confirmation payloads or mutation endpoints are needed to render the event.
