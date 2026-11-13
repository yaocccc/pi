# Tool-output filtering

[中文](README.md)

## Copy just this capability and its UI

Copy these **two complete directories**, keeping the relative layout below. No `shared/`, whole repository, top-level `pi-ui/index.ts`, or build step is required. Thin support code is already included.

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/filter-output" "$DEST/ui/filter-output"
cp -R "$SRC/filter-output/." "$DEST/filter-output/"
cp -R "$SRC/pi-ui/filter-output/." "$DEST/ui/filter-output/"
~~~

~~~text
my-extensions/
├── filter-output/index.ts        # capability, including all its local files
└── ui/filter-output/index.ts     # optional UI, including all its local files
~~~

**Native Pi, explicit entry points:**

~~~sh
pi --no-extensions -e "$DEST/filter-output/index.ts" -e "$DEST/ui/filter-output/index.ts"
~~~

**Or register native UI yourself**, for example in `$DEST/my-ui.ts`:

~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/filter-output/index.ts";
export default function (pi: ExtensionAPI) {
  registerUI(pi); // does not register/replace the capability
}
~~~

Load the capability and `my-ui.ts` instead of the UI entry above; do not load both registration paths or the whole-package adapter too. With automatic capability discovery, only add the UI registration. Without this UI, the capability still owns execution/validation; unsupported interactions must remain unavailable, never implicit consent.

For RPC, add `PI_UI_PROTOCOL=1` and `--mode rpc` to the explicit-entry command; the detailed wire contract follows below. Worker/Codex UI modules include their own optional method bridge and coordinate to register it once when combined. Other modules do not invent additional methods.

Pi 1.5.2 supplies the SDK, `pi-tui` and `typebox`; you do not need this repository's `node_modules` for these two directories. Use your own authentication, models and capability settings; copying source is not configuration. Keep directory names/layout as shown, or adjust imports yourself. Maintainers sync bundled helpers; consumers **do not** run generation scripts.

Filters selected successful tool results before model consumption. It replaces content for selected sensitive-file reads and applies common secret-pattern redactions to text. It is not a sandbox, comprehensive secret scanner, or guarantee that every display/log is sanitized.

## Third-party UI quick start (self-contained)
~~~sh
PI_UI_PROTOCOL=1 pi --mode rpc --no-extensions \
  -e /path/to/my-extensions/filter-output/index.ts \
  -e /path/to/my-extensions/ui/filter-output/index.ts
~~~
Use your own Pi credentials/settings. Keep all files inside the two copied directories; do not copy just index.ts. Node 22.19+ and the currently tested Pi 1.5.2 host are the baseline. PI_UI_PROTOCOL=1 opts into structured extension UI; the historical PI_GOUI_PROTOCOL=1 alias and pi-goui:* wire names remain compatible. No Go frontend is required. On older adapters that do not yet recognize PI_UI_PROTOCOL, replace that environment assignment with PI_GOUI_PROTOCOL=1; the wire payloads below are identical.

For installation without RPC transport, load only this capability; execution still works, but it emits only process-local facts. The optional pi-ui entry adapts facts to RPC/native presentation. This extension registers **no slash command, tool or remote control method**.

### Discover safely and receive facts
You may send {"id":"commands-1","type":"get_commands"} to discover the backend's commands, but its list cannot prove this passive capability exists. Do not send an invented /filter-output or pi-ui-rpc filter-output.get request as a probe. Remain a normal Pi UI if no fact arrives; no permanent feature control is needed.

With the structured marker, receive:
~~~json
{
  "type": "extension_ui_request",
  "id": "notice-1",
  "method": "notify",
  "notifyType": "info",
  "message": "pi-goui:event:v1:{\"name\":\"filter.redacted\",\"data\":{\"path\":\".env\"}}"
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
    return event.name === "filter.redacted" ? event.data : undefined;
  } catch { return undefined; }
}
~~~
## Scope and exact event meaning
There is no separate configuration file or public control service. Error results are not filtered. Reads of .env.example are explicitly passed through. Current sensitive-file patterns cover .env and selected variants, .dev.vars, secrets.{json,yaml,yml,toml} and credentials-prefixed paths. Pattern-based filtering also handles selected common credentials/private-key forms; this list is deliberately not a security guarantee.

filter.redacted carries **only** {path:string}, emitted when a sensitive read's content is replaced. It does not contain the file contents, a redaction count, tool-call ID or severity. Ordinary secret-pattern replacements in text currently do **not** emit this event; no event does not mean “unmodified” or “safe”.

Display path as untrusted text, not an instruction or automatically opened local file. Do not reread the file to expand this notice or expose original content. Continue rendering the actual standard tool result; do not synthesize a successful tool call or match one by guessing from this path. Unknown/missing details keep normal tool rendering.

A redaction notice is a fact, not a permission request. There is no answer payload, reveal-original action or filter.disable RPC method. Clearing its toast affects only the UI.

## Same-process adapter (without pi-ui)
Only an extension inside the same Pi process can subscribe to this bus:
~~~ts
import { FRONTEND_EVENT, type FrontendEventMessage } from "/path/to/my-extensions/filter-output/frontend.ts";
const unsubscribe = pi.events.on(FRONTEND_EVENT, (raw: unknown) => {
  const { context, event } = raw as FrontendEventMessage;
  if (event.name === "filter.redacted") {
    // Render event.data for context.sessionManager.getSessionId().
  }
});
~~~
Here the payload is {context,event}; remote clients receive only {name,data}. There is no getCapability service to invoke. Remove the listener on adapter disposal. UI observer failures must not affect the capability's outcome.

## Lifecycle, errors and minimal implementation
Route events to the source backend/session; clear stale toasts on session replacement and ignore unknown event names/versions or malformed payloads without showing raw protocol JSON. No stream of past notifications is replayed automatically. A lost connection or closed toast cannot roll back work already done by the capability. Do not fabricate a “cancel operation” API.

Keep ordinary RPC notify/tool/session behavior for missing adapters. Under generic RPC without the structured marker, this adapter does not export these machine-readable facts; use standard Pi state/results instead. Native Pi may receive a formatted notice.

**Minimal implementation:** standard Pi UI → optional fact parser → type-check expected data → display/refresh → dispose session-owned observers. No filesystem access, credentials, backend imports, confirmation payloads or mutation endpoints are needed to render the event.
