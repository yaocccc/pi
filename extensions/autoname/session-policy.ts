// Bundled support from shared/session-policy.ts; update via npm run sync:portable.
// Self-contained at runtime: consumers do not need the source template.
/** Background integrations target the user's primary session, regardless of UI.
 * Worker subprocesses set PI_WORKER_DEPTH explicitly and must not rename or
 * announce their temporary sessions as if they were user sessions.
 */
export function isPrimarySession(env: Record<string, string | undefined> = process.env): boolean {
	return !(Number(env.PI_WORKER_DEPTH ?? 0) > 0);
}
