// No Pi/model invocation: exercise the real ask_parent extension over inherited fd 3.
import askParentExtension from "../ask-parent.ts";
import { ChildChannel } from "../ipc.ts";
import { Socket } from "node:net";
import { spawn } from "node:child_process";
import { writeFileSync } from "node:fs";

const scenario = process.argv.includes("--mode") ? /"objective":\s*"([^"]+)"/.exec(process.argv.at(-1))?.[1] ?? "ask" : process.argv[2] || "ask";
const tools = new Map();
const handlers = new Map();
askParentExtension({ registerTool: (tool) => tools.set(tool.name, tool), on: (event, handler) => handlers.set(event, handler) });
const controller = new AbortController();
const emit = (message) => console.log(JSON.stringify(message));
if (scenario === "group-stubborn") {
	// Non-detached descendant, no inherited output pipes: leader close must not
	// be mistaken for group cleanup. Only the private readiness IPC is inherited.
	const grandchild = spawn(process.execPath, ["-e", 'process.on("SIGTERM", () => {}); setInterval(() => {}, 1000); process.send("ready"); process.disconnect();'], { detached: false, stdio: ["ignore", "ignore", "ignore", "ipc"] });
	await new Promise((resolve) => grandchild.once("message", resolve));
	writeFileSync("fixture-group.json", JSON.stringify({ leader: process.pid, grandchild: grandchild.pid }));
	setInterval(() => {}, 1000);
	emit({ type: "tool_execution_end", toolCallId: "fixture-ready", toolName: "fixture-ready", result: {} });
} else if (scenario === "hang" || scenario === "stubborn") {
	if (scenario === "stubborn") process.on("SIGTERM", () => {});
	setInterval(() => {}, 1000);
	emit({ type: "turn_start", turnIndex: 0 });
	emit({ type: "tool_execution_end", toolCallId: "fixture-ready", toolName: "fixture-ready", result: {} });
} else if (["ask", "catch", "exit", "question-timeout"].includes(scenario)) {
	if (scenario === "catch") process.on("SIGTERM", () => {});
	if (scenario === "exit") setTimeout(() => process.exit(2), 80);
	const result = await tools.get("ask_parent").execute("call-1", { question: "选择中文方案 A 还是 B?", timeoutMs: scenario === "question-timeout" ? 30 : 5_000 }, controller.signal)
		.catch((error) => ({ error: error.message }));
	emit({ type: "message_end", message: { role: "assistant", provider: "fixture", model: "local", content: [{ type: "text", text: JSON.stringify({ status: "completed", result }) }], usage: { input: 20, output: 10 } } });
	await handlers.get("session_shutdown")?.();
} else if (scenario === "broken-eof" || scenario === "broken-protocol") {
	process.on("SIGTERM", () => {});
	const socket = new Socket({ fd: 3, readable: true, writable: true });
	const channel = new ChildChannel(socket);
	const pending = channel.ask("Do not guess authorization").catch((error) => ({ error: error.message }));
	setTimeout(() => { if (scenario === "broken-eof") socket.end(); else socket.write("not-json\n"); }, 30);
	const result = await pending;
	emit({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: JSON.stringify({ status: "completed", result }) }] } });
	channel.close();
} else if (scenario === "two") {
	const channel = new ChildChannel(new Socket({ fd: 3, readable: true, writable: true }));
	const answers = await Promise.all([channel.ask("first?"), channel.ask("second?")]);
	emit({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: JSON.stringify(answers) }] } });
	channel.close();
} else {
	emit({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: JSON.stringify({ status: "completed" }) }] } });
}
