import { Socket } from "node:net";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";
import { ChildChannel, PARENT_FD_ENV, QUESTION_LIMIT, QUESTION_TIMEOUT_MS } from "./ipc.ts";

/** Explicitly loaded by runPiWorker; inactive in ordinary parent sessions. */
export default function askParentExtension(pi: ExtensionAPI) {
	if (process.env[PARENT_FD_ENV] !== "3" || Number(process.env.PI_WORKER_DEPTH || 0) < 1) return;
	let channel: ChildChannel | undefined;
	pi.on("session_shutdown", () => { channel?.close(); });
	pi.registerTool({
		name: "ask_parent",
		label: "Ask parent",
		description: "Ask the main Agent for a missing decision or clarification, not the end user. Waits for its answer while independent workers continue. Do not create other workers. Waiting retains your concurrency slot and path locks; timeout does not grant permission to guess or exceed your contract. Never send secrets.",
		parameters: Type.Object({
			question: Type.String({ minLength: 1, maxLength: QUESTION_LIMIT }),
			timeoutMs: Type.Optional(Type.Integer({ minimum: 1_000, maximum: 600_000, default: QUESTION_TIMEOUT_MS })),
		}),
		async execute(_id, { question, timeoutMs }, signal) {
			channel ??= new ChildChannel(new Socket({ fd: 3, readable: true, writable: true }));
			const answer = await channel.ask(question, signal, timeoutMs);
			return { content: [{ type: "text", text: answer.answer }], details: answer };
		},
	});
}
