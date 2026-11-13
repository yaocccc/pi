# ask-question capability

[中文](README.md) · [Installation](../README_EN.md)

## Copy just this capability and its UI

Copy these **two complete directories**, keeping the relative layout below. No `shared/`, whole repository, top-level `pi-ui/index.ts`, or build step is required. Thin support code is already included.

~~~sh
SRC=/path/to/repository/extensions
DEST=/path/to/my-extensions
mkdir -p "$DEST/ask-question" "$DEST/ui/ask-question"
cp -R "$SRC/ask-question/." "$DEST/ask-question/"
cp -R "$SRC/pi-ui/ask-question/." "$DEST/ui/ask-question/"
~~~

~~~text
my-extensions/
├── ask-question/index.ts        # capability, including all its local files
└── ui/ask-question/index.ts     # optional UI, including all its local files
~~~

**Native Pi, explicit entry points:**

~~~sh
pi --no-extensions -e "$DEST/ask-question/index.ts" -e "$DEST/ui/ask-question/index.ts"
~~~

**Or register native UI yourself**, for example in `$DEST/my-ui.ts`:

~~~ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import registerUI from "./ui/ask-question/index.ts";
export default function (pi: ExtensionAPI) {
  registerUI(pi); // does not register/replace the capability
}
~~~

Load the capability and `my-ui.ts` instead of the UI entry above; do not load both registration paths or the whole-package adapter too. With automatic capability discovery, only add the UI registration. Without this UI, the capability still owns execution/validation; unsupported interactions must remain unavailable, never implicit consent.

For RPC, add `PI_UI_PROTOCOL=1` and `--mode rpc` to the explicit-entry command; the detailed wire contract follows below. Worker/Codex UI modules include their own optional method bridge and coordinate to register it once when combined. Other modules do not invent additional methods.

Pi 1.5.2 supplies the SDK, `pi-tui` and `typebox`; you do not need this repository's `node_modules` for these two directories. Use your own authentication, models and capability settings; copying source is not configuration. Keep directory names/layout as shown, or adjust imports yourself. Maintainers sync bundled helpers; consumers **do not** run generation scripts.

`index.ts` registers the `ask_question` tool and its session-owned capability. It
contains no native renderer, dialog implementation or frontend-specific routing.
`model.ts` and `schema.ts` describe the inputs/results; `validation.ts` validates
answers from **every** frontend. `interaction.ts` owns FIFO ordering, pending
counts, cancellation and session shutdown.

## Public methods and information

Use `getCapability<AskQuestionService>(pi, ASK_QUESTION_SERVICE)` from
`./capabilities.ts`; the constants/interfaces are in `api.ts`.

- `ask({ questions, review, context, signal })` returns a validated submission.
  `undefined` means no compatible adapter is available. `{ cancelled: true,
  answers: [] }` means the user cancelled; unfinished drafts never count as consent.
- `pending()` includes the active request and all queued requests.
- `pi.events` emits `ask-question:pending` with `{ pending: number }`. It never
  broadcasts answers. Request lifetime is not extended by pending time.

The tool returns unanswered details with `unavailable: true` when no adapter is
installed; it does not claim that the user cancelled. Capabilities are discovered
on each call through the session's event bus, not shared module state.

## Frontend adapter port

Register a `QuestionInteractionPort` under `ASK_QUESTION_INTERACTION` using
`registerCapability`. `supports(context)` declares availability. `ask(request)`
receives a defensive copy of the normalized questions and a cancellation signal.
It must return `{ cancelled, answers }`; completed answers require `completed:
true`, selected strings and, for custom input, explicit `customAnswers` provenance.
The capability independently checks cardinality, option membership and provenance.

The adapter must close its own surface on abort. A faulty adapter that never
settles cannot hold the capability's pending state or shutdown open. The port is
an **in-process** integration contract; remote clients need an RPC/web bridge.

`../pi-ui/ask-question/index.ts` exports `registerAskQuestionUI(pi)`. It supplies
the optional native Pi renderer/dialogs and existing Go/generic RPC compatibility
transport. UI code depends on the capability, never the reverse. Another frontend
can implement the same port without loading `pi-ui`.

Tests:

```sh
node --experimental-transform-types --test extensions/ask-question/*.test.ts
env -u PI_UI_PROTOCOL -u PI_GOUI_PROTOCOL -u PI_GOUI_ASK_QUESTION node --experimental-transform-types --test extensions/pi-ui/ask-question/*.test.ts
```

## Third-party UI quick start (self-contained)

### Start and discover
~~~sh
PI_UI_PROTOCOL=1 pi --mode rpc --no-extensions \
  -e /path/to/my-extensions/ask-question/index.ts \
  -e /path/to/my-extensions/ui/ask-question/index.ts
~~~
Use your own Pi credentials/settings. Keep all files inside the two copied directories; do not copy just index.ts. Node 22.19+ and the currently tested Pi 1.5.2 host are the baseline. PI_UI_PROTOCOL=1 opts into structured extension UI; the historical PI_GOUI_PROTOCOL=1 alias and pi-goui:* wire names remain compatible. No Go frontend is required. On older adapters that do not yet recognize PI_UI_PROTOCOL, replace that environment assignment with PI_GOUI_PROTOCOL=1; the wire payloads below are identical.

This is a **tool-triggered interaction**, not an /ask_question command. You can send get_commands to discover adapter commands, but it does not list tools and must not be used to infer ask_question availability. When the tool actually runs, handle its standard extension_ui_request messages; otherwise keep the normal tool-result fallback. There is no remote ask-question.ask RPC method.

### Route A: ordinary Pi dialogs
For the least integration work, omit PI_UI_PROTOCOL, PI_GOUI_PROTOCOL and PI_GOUI_ASK_QUESTION. Implement standard select/input/notify dialogs. Return the selected **exact option string** via {"type":"extension_ui_response","id":"…","value":"…"}; input returns text, cancellation uses cancelled:true. Multi-select is an adapter-owned series of select/input requests, not a single select containing an array. Treat labels as opaque; do not parse Chinese labels into actions yourself. One question's completion may open the next request with a new id.

### Route B: one structured questionnaire
With the structured marker above, receive:
~~~json
{
  "type": "extension_ui_request",
  "id": "q1",
  "method": "input",
  "title": "pi-goui:ask-question:v1",
  "placeholder": "{\"questions\":[{\"question\":\"Choose a target\",\"label\":\"Target\",\"options\":[\"A\",\"B\"],\"multiSelect\":false}],\"review\":true}"
}
~~~

Parse placeholder as JSON, render questions in order, and respect review: do not treat an unfinished page/draft as final consent. questions has 1–8 items. Options have already been normalized (whitespace cleaned, empty/duplicate options removed, maximum 8). Send the **whole submission JSON encoded as the string value**, not an object in value:
~~~json
{
  "type": "extension_ui_response",
  "id": "q1",
  "value": "{\"cancelled\":false,\"answers\":[{\"completed\":true,\"answers\":[\"B\"],\"wasCustom\":false}]}"
}
~~~

Each answer requires completed:true and a nonempty, duplicate-free answers array. Single-select accepts exactly one answer; multi-select accepts one or more. Every value not among options must be explicitly listed in customAnswers. wasCustom means all selected answers are custom, not merely that some custom text was entered. Example submission for a multi-select question with option A:
~~~json
{
  "cancelled": false,
  "answers": [
    {
      "completed": true,
      "answers": [
        "A",
        "my target"
      ],
      "wasCustom": false,
      "customAnswers": [
        "my target"
      ]
    }
  ]
}
~~~

There is no customInput wire field. A frontend's text editor value must become answers plus customAnswers, with wasCustom set consistently. The capability validates membership, cardinality, provenance and completion independently; malformed/mismatched submissions produce a tool error (including "Invalid questionnaire response"), never silently accepted consent.

Cancel the current request:
~~~json
{
  "type": "extension_ui_response",
  "id": "q1",
  "cancelled": true
}
~~~

Alternatively the submission value may encode {"cancelled":true,"answers":[]}. Do not send partial completed answers as a successful submission. The capability owns FIFO and pending counts, and cancels outstanding work on session shutdown. A remote UI should discard pending request IDs on backend exit/session replacement, honor supplied timeouts, and never replay a stale answer into a different session. The Pi standard RPC dialog protocol does not promise a separate remote close-dialog event for every abort.

### Render the authoritative result
Use tool_execution_update.partialResult.details / tool_execution_end.result.details, or a historical toolResult message's details, not prose parsing. A grouped result looks like:
~~~json
{
  "questions": [
    {
      "question": "Choose a target",
      "label": "Target",
      "options": [
        "A",
        "B"
      ],
      "answer": "B",
      "multiSelect": false,
      "wasCustom": false
    }
  ]
}
~~~

The legacy single-question shape is {question,options,answer,multiSelect,wasCustom?,customAnswers?}; answer is a string for single-select, an array for multi-select, or null when unanswered. Grouped details contain questions and optional cancelled/unavailable. unavailable:true means no compatible UI adapter; cancelled:true means cancellation. Neither means approval. Unknown/missing details retain generic tool output.

### Implement an in-process adapter instead
Only a TypeScript extension running in the **same Pi process** can use this port. Do not load pi-ui's questionnaire adapter at the same time as your replacement. Import paths below refer to this installed package:
~~~ts
import { registerCapability, getCapability } from "/path/to/my-extensions/ask-question/capabilities.ts";
import { ASK_QUESTION_INTERACTION, ASK_QUESTION_SERVICE, type QuestionInteractionPort, type AskQuestionService } from "/path/to/my-extensions/ask-question/api.ts";
// Inside your default extension factory(pi):
registerCapability<QuestionInteractionPort>(pi, ASK_QUESTION_INTERACTION, {
  supports: (context) => myFrontend.isConnected(context),
  ask: ({ questions, review, context, signal }) =>
    myFrontend.showForm({ questions, review, context, signal }),
});
const service = getCapability<AskQuestionService>(pi, ASK_QUESTION_SERVICE);
~~~

myFrontend is your own UI implementation, not a Pi API. showForm must return the same submission shape above and close its surface when signal aborts. The service exposes ask({questions,review,context,signal}) and pending(); ask returns undefined if no compatible port exists. Subscribe via pi.events.on("ask-question:pending", ({pending}) => …); this is a process-local count, not a broadcast of answers and not automatically exported over RPC.

**Minimal implementation order:** standard dialogs → structured form → authoritative tool-result rendering → abort/session cleanup. The pi-goui:ask-question:v1 suffix is the protocol version; ignore unsupported future versions and fall back to a standard input/editor instead of guessing their schema.
