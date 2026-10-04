/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: MIT
 */

import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { ExtensionContext } from "@earendil-works/pi-coding-agent";

const temporaryRoots = new Map<string, string>();

export function runtimeRoot(ctx: ExtensionContext): string {
	const sessionDir = ctx.sessionManager.getSessionDir();
	const sessionId = ctx.sessionManager.getSessionId();
	if (!/^[a-z0-9][a-z0-9._-]*$/iu.test(sessionId)) {
		throw new Error("SoL-Pi requires a safe Pi session id");
	}
	if (sessionDir) return join(sessionDir, "sol-pi", sessionId);

	// Pi 1.0.2 --no-session / SessionManager.inMemory() expose an empty sessionDir.
	// Share a private, unpredictable root across contexts for exact recall. Keep
	// files after shutdown/session replacement so parent processes can read them.
	let root = temporaryRoots.get(sessionId);
	if (!root) {
		root = mkdtempSync(join(tmpdir(), `sol-pi-${sessionId}-`));
		temporaryRoots.set(sessionId, root);
	}
	return root;
}
