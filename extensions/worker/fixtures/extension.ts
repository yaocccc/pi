// Loaded by the installed Pi loader. Real extension/runtime/executeTask, fake model configuration only.
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import workerExtension from "../index.ts";
import { executeTask } from "../process.ts";
import { DEFAULT_OPTIONS } from "../config.ts";
import { WorkerRuntime } from "../runtime.ts";
import { WORKER_USAGE_EVENT } from "../events.ts";

const start = WorkerRuntime.prototype.start;
WorkerRuntime.prototype.start = function (...args) {
	((globalThis as any).__workerFixtureRuntimes ??= new Set()).add(this);
	return start.apply(this, args);
};

export default function fixtureExtension(pi: ExtensionAPI) {
	const state = globalThis as any;
	(state.__workerFixtureModules ??= new Set()).add(executeTask);
	const preset = { model: "fixture/local", thinking: "high" as const };
	const emit = pi.events.emit.bind(pi.events);
	pi.events.emit = (event, payload) => {
		if (event === WORKER_USAGE_EVENT && state.__workerFixtureThrowUsage) {
			state.__workerFixtureUsageThrows = (state.__workerFixtureUsageThrows ?? 0) + 1;
			throw new Error("fixture presentation usage failure");
		}
		return emit(event, payload);
	};
	workerExtension(pi, {
		executeTask: (...args: Parameters<typeof executeTask>) => {
			(state.__workerFixtureOwners ??= new Set()).add(args[9]);
			const progress = args[5];
			args[5] = (patch) => {
				if (patch.activities?.some((activity) => activity.id === "tool:fixture-ready")) (state.__workerFixtureReady ??= new Set()).add(args[9]?.id);
				if (state.__workerFixtureThrowProgress && patch.phase === state.__workerFixtureThrowProgress) {
					state.__workerFixtureProgressThrows = (state.__workerFixtureProgressThrows ?? 0) + 1;
					throw new Error("fixture onProgress failure");
				}
				progress?.(patch);
			};
			return executeTask(...args);
		},
		loadRoutingConfig: () => ({ config: { ...DEFAULT_OPTIONS, maxConcurrentWorkers: 1, defaultTimeoutMs: 15_000, fast: preset, normal: preset, deep: preset, max: preset }, warnings: [], path: "fixture" }),
	});
}
