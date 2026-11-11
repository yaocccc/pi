/** Facts shared by frontends; text, severity and display policy belong to each UI. */
export type FrontendEvent =
	| { name: "autoname.renamed"; data: { name: string; contextTokens: number; durationMs: number } }
	| { name: "filter.redacted"; data: { path: string } }
	| { name: "context.ready"; data: { generationMs: number } }
	| { name: "context.applied"; data: { beforeTokens: number; afterTokens: number; generationMs: number; durationMs: number } }
	| { name: "context.failed"; data: { durationMs: number; reason: string } }
	| { name: "worker.settings-saved"; data: Record<string, never> }
	| { name: "worker.settings-error"; data: { message: string } };
