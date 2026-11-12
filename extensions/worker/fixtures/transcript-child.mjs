// Local protocol fixture; no agent/model/network calls.
const [scenario = "complete", prompt = "inspect"] = process.argv.slice(2);
const emit = (event) => process.stdout.write(JSON.stringify(event) + "\n");
emit({ type: "session", version: 3, id: "child-session-header-not-a-viewer-log" });
emit({ type: "agent_start" });
const user = { role: "user", content: prompt, timestamp: 1 };
emit({ type: "message_start", message: user });
emit({ type: "message_end", message: user });
emit({ type: "message_start", message: { role: "assistant", content: [], timestamp: 2 } });
const delta = Buffer.from(JSON.stringify({ type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: "中文 🧪\u2028完整" } }) + "\n");
const at = delta.indexOf(Buffer.from("中")) + 1;
process.stdout.write(delta.subarray(0, at));
setTimeout(() => {
 process.stdout.write(delta.subarray(at));
 emit({ type: "tool_execution_start", toolCallId: "call-1", toolName: "read", args: { path: "file.go" } });
 if (scenario === "wait") { setInterval(() => {}, 1000); return; }
 emit({ type: "tool_execution_update", toolCallId: "call-1", toolName: "read", partialResult: { content: [{ type: "text", text: "live" }] } });
 emit({ type: "tool_execution_end", toolCallId: "call-1", toolName: "read", result: { content: [{ type: "text", text: "full content" }], details: { complete: true } }, isError: false });
 emit({ type: "message_end", message: { role: "toolResult", toolCallId: "call-1", toolName: "read", content: [{ type: "text", text: "full content" }], timestamp: 3 } });
 emit({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: "中文 🧪\u2028完整" }], timestamp: 4 } });
 if (scenario === "bad-json") process.stdout.write("not-json\n");
 emit({ type: "agent_end", messages: [] });
 emit({ type: "agent_settled", aborted: false });
 if (scenario === "fail") process.exitCode = 7;
}, 15);
