/** Backend facts owned by this capability; UI owns their presentation. */
export type FrontendEvent =
	| { name: "autoname.renamed"; data: { name: string; contextTokens: number; durationMs: number } };
