/** Compatibility aggregate; each capability owns its standalone event contract. */
export type FrontendEvent =
	| import("../autoname/frontend.ts").FrontendEvent
	| import("../filter-output/frontend.ts").FrontendEvent
	| import("../sol-pi/frontend.ts").FrontendEvent
	| import("../worker/frontend.ts").FrontendEvent;
