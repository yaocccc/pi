// Model-free child emitting the same JSON event stream as the Pi CLI.
import { spawn } from "node:child_process";
import { writeFileSync } from "node:fs";

const scenario = process.argv.includes("--mode") ? /"objective":\s*"([^"]+)"/.exec(process.argv.at(-1))?.[1] ?? "done" : process.argv[2] || "done";
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

} else {
	if (scenario === "tool-tokens") {
		const args = { path: "example.ts" };
		const result = { content: [{ type: "text", text: "x".repeat(3000) }], isError: false };
		emit({ type: "tool_execution_start", toolCallId: "read-1", toolName: "read", args });
		emit({ type: "tool_execution_end", toolCallId: "read-1", toolName: "read", result });
		emit({ type: "message_end", message: { ...result, role: "toolResult", toolCallId: "read-1", toolName: "read" } });
	}
	emit({ type: "message_end", message: { role: "assistant", provider: "fixture", model: "local", content: [{ type: "text", text: JSON.stringify({ status: scenario === "blocked" ? "blocked" : "completed", summary: [scenario === "blocked" ? "Required decision is missing" : "done"] }) }], usage: { input: 20, output: 10 } } });
}
