// No Pi/model invocation: exercise the real ask_parent extension over inherited fd 3.
import askParentExtension from "../ask-parent.ts";
import { ChildChannel } from "../ipc.ts";
import { Socket } from "node:net";

const scenario = process.argv[2] || "ask";
const tools = new Map();
const handlers = new Map();
askParentExtension({ registerTool: (tool) => tools.set(tool.name, tool), on: (event, handler) => handlers.set(event, handler) });
const controller = new AbortController();
const emit = (message) => console.log(JSON.stringify(message));
if (scenario === "hang" || scenario === "stubborn") {
	if (scenario === "stubborn") process.on("SIGTERM", () => {});
	setInterval(() => {}, 1000);
	emit({ type: "turn_start", turnIndex: 0 });
} else if (scenario === "ask" || scenario === "exit" || scenario === "question-timeout" || scenario === "human-wait") {
	if (scenario === "exit") setTimeout(() => process.exit(2), 80);
	const result = await tools.get("ask_parent").execute("call-1", { question: "选择中文方案 A 还是 B?", timeoutMs: scenario === "question-timeout" ? 30 : scenario === "human-wait" ? 300 : 5_000 }, controller.signal)
		.catch((error) => ({ error: error.message }));
	emit({ type: "message_end", message: { role: "assistant", provider: "fixture", model: "local", content: [{ type: "text", text: JSON.stringify({ status: "completed", result }) }], usage: { input: 20, output: 10 } } });
	await handlers.get("session_shutdown")?.();
} else if (scenario === "two") {
	const channel = new ChildChannel(new Socket({ fd: 3, readable: true, writable: true }));
	const answers = await Promise.all([channel.ask("first?"), channel.ask("second?")]);
	emit({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: JSON.stringify(answers) }] } });
	channel.close();
} else {
	emit({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: JSON.stringify({ status: "completed" }) }] } });
}
