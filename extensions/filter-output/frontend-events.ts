/** Backend facts owned by this capability; UI owns their presentation. */
export type FrontendEvent =
	| { name: "filter.redacted"; data: { path: string } };
