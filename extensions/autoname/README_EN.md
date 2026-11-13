# Automatic Session Naming

[中文](README.md)

Names primary sessions after agent_settled, subject to a cooldown; does not run in Workers. The capability owns naming, model calls, and session writes; UI only presents successful facts.

## Installation

Copy both complete directories and preserve their relative layout. Helpers are included; no shared directory, node_modules, build, or author configuration is needed. Pi supplies SDK dependencies.

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extension
mkdir -p "$DEST/autoname" "$DEST/ui/autoname"
cp -R "$SRC/autoname/." "$DEST/autoname/"
cp -R "$SRC/pi-ui/autoname/." "$DEST/ui/autoname/"
pi --no-extensions -e "$DEST/autoname/index.ts" -e "$DEST/ui/autoname/index.ts"
~~~

Alternatively register from your own entry, without separately loading the UI entry:
~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/autoname/index.ts";

export default function (pi: ExtensionAPI) {
  registerUI(pi);
}
~~~

The capability can name sessions without UI; notifications require its adapter. There is no dedicated tool, slash command, or remote method.

## Configuration and privacy

autoname.json in the current Pi agent directory accepts these fields. Missing or unreadable files use defaults without creating or rewriting the configuration:
~~~json
{"enabled":true,"notify":true,"cooldownSeconds":600,"model":"auto","reasoning":"minimal"}
~~~

- model="auto" uses the current session model; alternatively select a configured provider/model. Without a provider, resolution tries a unique model-name match. Supply your own credentials.
- reasoning accepts minimal/low/medium/high/xhigh/max. Cooldown starts at the attempt, including failures. An existing name changes only when a topic shift is detected; manual renames, branch/turn changes, and stale results during the request are protected.
- Naming reads current-branch context, calls a model, and may incur extra charges. notify=false only hides notices; use enabled=false to stop naming requests.
- Missing models/credentials or naming failures do not block normal conversation. The capability persists names and attempts without accessing terminals, toasts, or editors.

## Third-party UI: names and facts

The standard Pi RPC get_state response includes sessionName; use it when restoring a page rather than reconstructing names from notification logs:
~~~json
{"id":"state-1","type":"get_state"}
~~~

With the corresponding UI loaded, successful naming with notify=true emits:
~~~json
{"type":"extension_ui_request","id":"notice-name-1","method":"notify","notifyType":"info","message":"pi-goui:event:v1:{\"name\":\"autoname.renamed\",\"data\":{\"name\":\"Review worker scheduling\",\"contextTokens\":2048,\"durationMs\":820}}"}
~~~

Strip only the exact pi-goui:event:v1: prefix and parse JSON. name is the session name; contextTokens and durationMs describe this naming request, not total primary-session usage. Do not send extension_ui_response. Update only the owning backend/session, do not replay historical toasts, and ignore malformed/unknown events. A missing notice does not prove naming failed.

Manual renaming uses standard Pi RPC set_session_name, not an invented /autoname command:
~~~json
{"id":"name-1","type":"set_session_name","name":"My explicit title"}
~~~

In-process hosts may subscribe to pi:frontend-event:v1, filtering {context,event} by event.name="autoname.renamed". Correlate ownership through context and unsubscribe on unload. Facts follow persistence; observer failures do not change naming outcomes. Without an adapter, retain standard session-name display. Do not read autoname.json from the frontend.

See [config.ts](config.ts), [helpers.ts](helpers.ts), and [index.ts](index.ts) for naming/configuration boundaries, and [pi-ui](../pi-ui/README_EN.md) for common UI rules.
