import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

/** In-process discovery over the session's event bus, never a module-global registry.
 * Providers expose data/methods; callers decide when to use them. Discovery is
 * synchronous, so it is independent of extension load order once loading finishes.
 * Remote frontends should bridge these methods rather than share configuration files.
 */
const CHANNEL = "pi:capability:discover:v1";
type Host = Pick<ExtensionAPI, "events">;
interface Discovery { name: string; provide(value: unknown): void }

export function registerCapability<T>(pi: Host, name: string, value: T): () => void {
	return pi.events.on(CHANNEL, (data) => {
		const request = data as Discovery;
		if (request.name === name) request.provide(value);
	});
}

export function getCapability<T>(pi: Host, name: string): T | undefined {
	let value: T | undefined;
	pi.events?.emit(CHANNEL, { name, provide(candidate: T) { value ??= candidate; } } satisfies Discovery);
	return value;
}

const ACTIVITY_CHANNEL = "pi:session:activity:v1";
export interface SessionActivity { name: string; reason?: string }
interface ActivityQuery { report(activity: SessionActivity): void }

/** Observe detached work without cancelling it or starting a new agent turn.
 * Providers keep reporting activity through asynchronous cancellation/cleanup. */
export function registerSessionActivity(pi: Host, name: string, reason: () => string | undefined): () => void {
	return pi.events.on(ACTIVITY_CHANNEL, (data) => {
		const query = data as ActivityQuery;
		let value: unknown;
		try { value = reason(); }
		catch { value = "activity could not be confirmed"; }
		query.report({ name, ...(value === undefined ? {} : {
			reason: typeof value === "string" && value.trim() ? value.slice(0, 512) : "activity could not be confirmed",
		}) });
	});
}

export function sessionActivities(pi: Host): SessionActivity[] {
	const activities: SessionActivity[] = [];
	pi.events.emit(ACTIVITY_CHANNEL, { report: (activity: SessionActivity) => activities.push(activity) } satisfies ActivityQuery);
	return activities;
}
