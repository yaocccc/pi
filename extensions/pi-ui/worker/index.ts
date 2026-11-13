import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { getCapability } from "../../worker/capabilities.ts";
import { notifyFrontendEvent } from "../../worker/frontend.ts";
import { WORKER_CAPABILITY, type WorkerCapability, type WorkerSessionView } from "../../worker/api.ts";
import { nativeWorkerRenderers } from "./native-ui.ts";
import { WorkerRenderRefresh } from "./presentation.ts";
import { registerExtensionRpc, registerRpcMethod, RpcUnavailableError } from "./bridge.ts";
import { registerFactUI } from "./support.ts";
import { eventNames, nativeEventNotice } from "./events.ts";

export default registerWorkerUI;

/** Optional adapter: the capability owns sessions; only this module owns rows/timers. */
export function registerWorkerUI(pi: ExtensionAPI) {
	registerExtensionRpc(pi);
	registerFactUI(pi, eventNames, nativeEventNotice);
	const adapters = new WeakMap<WorkerSessionView, { refresh: WorkerRenderRefresh; dispose(): void }>();
	const emptySession: WorkerSessionView = { awaitingHistory: false, closed: true, lookup: () => undefined, history: () => undefined, subscribe: () => () => {} };
	const service = () => getCapability<WorkerCapability>(pi, WORKER_CAPABILITY);
	registerRpcMethod(pi, "worker.transcript", (params) => {
		const capability = service();
		if (!capability) throw new RpcUnavailableError("Worker capability unavailable");
		return capability.readTranscript(params);
	});
	const getSession = () => {
		const session = service()?.getSession() ?? emptySession;
		let adapter = adapters.get(session);
		if (!adapter) {
			const refresh = new WorkerRenderRefresh((id) => session.lookup(id), undefined, (id) => session.history(id), session.awaitingHistory);
			let unsubscribe = () => {};
			const dispose = () => { refresh.dispose(); unsubscribe(); };
			adapter = { refresh, dispose };
			adapters.set(session, adapter);
			if (session.closed) dispose();
			else {
				unsubscribe = session.subscribe((change) => {
					if (change.type === "closed") dispose();
					else if (change.type === "history") refresh.bindHistory();
					else if (change.type === "batch") refresh.refreshBatch(change.batchId);
					else refresh.changed();
				});
			}
		}
		return { lookup: (id: string) => session.lookup(id), history: (id: string) => session.history(id), refresh: adapter.refresh };
	};
	const renderers = nativeWorkerRenderers(getSession);
	pi.registerToolRenderer((name, next) => name === "worker" ? renderers : next());
	// Dispose on the session-bound capability's closed event, after its final
	// cleanup snapshot. A UI session_shutdown hook would run too early when the
	// adapter loads before Worker; this ordering needs no frontend lifecycle hook.
	pi.registerCommand("worker_settings", {
		description: "交互式配置 Worker",
		handler: async (_args, ctx) => {
			if (!ctx.hasUI) {
				pi.sendMessage({ customType: "text", content: "/worker_settings 仅支持交互式 UI。", display: true });
				return;
			}
			await ctx.waitForIdle();
			try {
				const capability = service();
				if (!capability) throw new Error("Worker capability unavailable");
				const { configureWorkerSettings } = await import("./settings.ts");
				await configureWorkerSettings(ctx, capability);
			} catch (error) {
				notifyFrontendEvent(pi, ctx, { name: "worker.settings-error", data: { message: error instanceof Error ? error.message : String(error) } });
			}
		},
	});
}
