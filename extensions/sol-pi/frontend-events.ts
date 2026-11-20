/** Backend facts owned by this capability; UI owns their presentation. */
export type FrontendEvent =
	| { name: "context.ready"; data: { generationMs: number } }
	| { name: "context.applied"; data: { beforeTokens: number; afterTokens: number; generationMs: number; durationMs: number } }
	| { name: "context.failed"; data: { durationMs: number; reason: string } };
