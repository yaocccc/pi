/** Backend facts owned by this capability; UI owns their presentation. */
export type FrontendEvent =
	| { name: "worker.settings-saved"; data: Record<string, never> }
	| { name: "worker.settings-error"; data: { message: string } };
