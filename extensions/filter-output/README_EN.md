# Sensitive Tool-Result Filtering

[中文](README.md)

Masks selected sensitive text in successful tool results and hides whole read results for designated sensitive files. It does not change execution, inputs, files on disk, or failed results, and is not a complete data-loss prevention system.

## Installation

Copy both complete directories and preserve their relative layout. Helpers are included; no shared directory, node_modules, build, or author configuration is needed. Pi supplies SDK dependencies.

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extension
mkdir -p "$DEST/filter-output" "$DEST/ui/filter-output"
cp -R "$SRC/filter-output/." "$DEST/filter-output/"
cp -R "$SRC/pi-ui/filter-output/." "$DEST/ui/filter-output/"
pi --no-extensions -e "$DEST/filter-output/index.ts" -e "$DEST/ui/filter-output/index.ts"
~~~

Alternatively register from your own entry, without separately loading the UI entry:
~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/filter-output/index.ts";

export default function (pi: ExtensionAPI) {
  registerUI(pi);
}
~~~

Filtering is UI-independent; only notices require the adapter. There is no configuration file, dedicated tool, slash command, or remote method.

## Scope and limitations

- read matches for .env/.env.*, .dev.vars*, secret(s).json/yaml/yml/toml, or credentials-prefixed filenames replace the entire content with a safe placeholder and publish filter.redacted; original line structure is not retained. Reading .env.example skips processing, not every path ending in example.
- Other successful results—including ordinary read, bash, and other tools—apply heuristics to text blocks for selected key prefixes, token/password assignments, database credentials, private keys, and similar patterns. Non-text blocks are preserved; changed text produces a replacement result but no dedicated fact.
- Unknown formats, images, and errors are not guaranteed coverage, and ordinary text may be misclassified. Raw data may remain on disk, in archives, or in other logs. This does not replace authorization, access control, credential rotation, or manual review.

[index.ts](index.ts) is authoritative. Frontends must not rerun masking or try to restore text.

## Third-party UI: consume processed results only

Render results from standard Pi tool events/messages; no extension-specific call is needed. Missing adapters or notices do not mean filtering is disabled.

With the corresponding UI loaded, hiding a sensitive-file read result emits the following; other text masking does not:
~~~json
{"type":"extension_ui_request","id":"notice-filter-1","method":"notify","notifyType":"warning","message":"pi-goui:event:v1:{\"name\":\"filter.redacted\",\"data\":{\"path\":\".env\"}}"}
~~~

Strip only the exact pi-goui:event:v1: prefix and parse JSON. data.path identifies the hidden file; consider path privacy when displaying it. Facts omit file contents and recovery handles. Do not send extension_ui_response; ignore unknown/malformed events and do not replay historical notices as new warnings.

In-process hosts may subscribe to pi:frontend-event:v1, filtering {context,event} by event.name="filter.redacted". Use context for ownership and unsubscribe on unload. Observer failures do not affect filtering. There is no UI protocol for toggles, rule management, raw export, or show-original.

See [pi-ui](../pi-ui/README_EN.md) for common boundaries. This event only reports a hidden sensitive-file result, not proof that all results are completely redacted.
