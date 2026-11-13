import assert from "node:assert/strict";
import test from "node:test";
import { createEventBus } from "@earendil-works/pi-coding-agent";
import { registerCapability } from "../../shared/capabilities.ts";
import { WORKER_CAPABILITY } from "../../worker/api.ts";
import { registerWorkerUI } from "./index.ts";
import { EXTENSION_RPC_PREFIX, registerExtensionRpc } from "../rpc.ts";

test("Worker transcript RPC is optional and delegates all reading and validation", async () => {
	const events = createEventBus();
	const commands = new Map<string, any>();
	const pi: any = { events, registerCommand: (name: string, command: unknown) => commands.set(name, command), registerToolRenderer() {} };
	registerExtensionRpc(pi);
	registerWorkerUI(pi); // UI may load before the Worker capability.
	const notices: string[] = [];
	const ctx: any = { mode: "rpc", hasUI: true, ui: { notify: (message: string) => notices.push(message) } };
	const call = async (params: unknown) => {
		await commands.get("pi-ui-rpc").handler(JSON.stringify({ id: "request", method: "worker.transcript", params }), ctx);
		return JSON.parse(notices.pop()!.slice(EXTENSION_RPC_PREFIX.length));
	};
	assert.equal((await call({})).error.code, "unavailable");
	let received: unknown;
	registerCapability(pi, WORKER_CAPABILITY, { readTranscript(request: unknown) { received = request; return { cursor: "opaque", records: [], reset: true, partial: false, more: false }; } });
	const params = { descriptor: { version: 1, id: "backend-validates-this" }, cursor: "opaque-old" };
	assert.equal((await call(params)).result.cursor, "opaque");
	assert.deepEqual(received, params);
	events.clear();
});
