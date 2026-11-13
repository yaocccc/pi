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
