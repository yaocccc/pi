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

For RPC, add `--mode rpc` to the explicit-entry command. See the wire examples below and the [shared RPC contract](../pi-ui/README_EN.md).

Pi 1.5.2 supplies the SDK, `pi-tui` and `typebox`; you do not need this repository's `node_modules` for these two directories. Use your own authentication, models and capability settings; copying source is not configuration. Keep directory names/layout as shown, or adjust imports yourself.

Load `ask-question/index.ts` for the `ask_question` tool and the matching UI entry shown above (or your own interaction port). No separate configuration file is required.

The capability owns normalization, FIFO ordering, pending counts, cancellation, session shutdown and authoritative validation for every frontend. Without a compatible adapter, the tool returns unanswered details with `unavailable: true`, not cancellation or consent.

## Tool usage

Call `ask_question` with `question` + `options` for one question, or `questions` for 1–8 questions. Each question has 1–8 options, optional `multiSelect` (default false), and an optional short `label` inside `questions`. Custom input is added by the UI.

~~~json
{"questions":[{"question":"Choose a target","options":["A","B"],"label":"Target","multiSelect":false}]}
~~~

## Interface

Discover `ask-question:v1` with `getCapability(pi, ASK_QUESTION_SERVICE)` from `./capabilities.ts`; interfaces are in `api.ts`.

- `ask({ questions, review, context, signal })` returns validated answers; undefined means unavailable, cancellation returns `{cancelled:true, answers:[]}`.
- `pending()` includes active and queued requests.
- `ask-question:pending` publishes counts, never answers; waiting does not extend task deadlines.
- Register `ask-question.interaction:v1` with `supports(context)` and `ask(request)`; it receives a defensive copy of normalized questions and a cancellation signal.
- Submissions must specify completion, selected answers and custom-input provenance; the capability revalidates rather than trusting the UI.

Adapters close their own surface on abort. A broken adapter cannot indefinitely hold pending state or shutdown open. Drafts are not consent. Native and RPC transports live in pi-ui; retain all files in both copied directories.

## Third-party UI quick start (self-contained)

### Start and discover
Requires Node 22.19+ and Pi 1.5.2. RPC exports structured messages by default without protocol environment variables; `pi-goui:*` names do not require Go. Implement the questionnaire protocol below; standard-dialog-only clients need updating.

This is a **tool-triggered interaction**, not an /ask_question command. You can send get_commands to discover adapter commands, but it does not list tools and must not be used to infer ask_question availability. When the tool actually runs, handle its standard extension_ui_request messages; otherwise keep the normal tool-result fallback. There is no remote ask-question.ask RPC method.

### RPC protocol requirement
RPC no longer offers an environment-selected standard select/input questionnaire fallback. Implement the structured questionnaire protocol below and submit all answers together; do not display the protocol JSON as an ordinary input prompt.

### One structured questionnaire
RPC receives by default:
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

The `pi-goui:ask-question:v1` suffix is the protocol version. Do not guess unknown schemas or submit fabricated answers; keep generic tool-result rendering and mark unsupported interaction unavailable.
