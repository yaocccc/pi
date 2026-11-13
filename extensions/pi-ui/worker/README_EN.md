# worker UI

[中文](README.md)

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

For RPC, add `PI_UI_PROTOCOL=1` and `--mode rpc` to the explicit-entry command; the capability guide contains the full wire contract. Worker/Codex UI modules include their own optional method bridge and coordinate to register it once when combined. Other modules do not invent additional methods.

Pi 1.5.2 supplies the SDK, `pi-tui` and `typebox`; you do not need this repository's `node_modules` for these two directories. Use your own authentication, models and capability settings; copying source is not configuration. Keep directory names/layout as shown, or adjust imports yourself. Maintainers sync bundled helpers; consumers **do not** run generation scripts.

Full backend/UI contract: [worker](../../worker/README_EN.md).
