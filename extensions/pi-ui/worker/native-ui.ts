/** Native Pi adapter. RPC clients render worker-ui data independently. */
import type { ToolRenderers } from "@earendil-works/pi-coding-agent";
import { Text } from "@earendil-works/pi-tui";
import type { WorkerUiDetails } from "../../worker/types.ts";
import { emptyWorkerComponent, type WorkerRenderRefresh } from "./presentation.ts";
import { renderWorkerDetails } from "./ui.ts";

interface NativeWorkerSession {
	lookup(batchId: string): WorkerUiDetails | undefined;
	history(batchId: string): WorkerUiDetails | undefined;
	refresh: WorkerRenderRefresh;
}

type NativeRenderInput = { tasks?: unknown[]; batchId?: string; cancel?: true };
export function nativeWorkerRenderers(getSession: () => NativeWorkerSession): ToolRenderers {
	return {
		renderShell: "self",
		renderCall(args, theme) {
			// Empty partial args and ALL continuations have no title, shell, padding or spinner.
			const input = args as NativeRenderInput | undefined;
			if (!Array.isArray(input?.tasks)) return emptyWorkerComponent();
			const count = input.tasks.length;
			const title = count > 1 ? `Workers ×${count}` : "Worker";
			return new Text(theme.fg("toolTitle", theme.bold(title)), 0, 0);
		},
		renderResult(result, options, theme, context) {
			const session = getSession();
			const details = result.details as WorkerUiDetails | undefined;
			const batchId = details?.batchId ?? (context?.args as NativeRenderInput | undefined)?.batchId;
			const known = batchId ? session.lookup(batchId) ?? session.history(batchId) : undefined;
			const origin = known?.originToolCallId ?? details?.originToolCallId;
			if (context?.toolCallId && origin && context.toolCallId !== origin && known &&
				(details?.kind === "worker-ui" || known.controlErrors?.some((error) => error.toolCallId === context.toolCallId))) return emptyWorkerComponent();
			if (details?.kind === "worker-ui" && details.tasks.length) {
				const view = session.refresh.resolve(details, context);
				return renderWorkerDetails(view.details, theme, { expanded: options.expanded, snapshot: view.snapshot });
			}
			const text = result.content.find((item) => item.type === "text");
			return new Text(text?.type === "text" ? text.text : "Worker 无输出", 0, 0);
		},
	};
}
