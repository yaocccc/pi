/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: MIT
 */

import type { Component } from "@earendil-works/pi-tui";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

import { registerFactUI } from "./support.ts";
import { eventNames, nativeEventNotice } from "./events.ts";

export function registerSolPiUI(pi: ExtensionAPI): void {
	registerFactUI(pi, eventNames, nativeEventNotice);
	pi.registerToolRenderer((name, next) => name === "obs_recall" || name === "update_plan"
		? { renderShell: "self", renderCall: hiddenRenderer, renderResult: hiddenRenderer }
		: next());
}

/** Background tools retain model-visible results without adding terminal cards. */
export function hiddenRenderer(): Component {
	return {
		render: (_width) => [],
		invalidate() {},
	};
}

export default registerSolPiUI;
