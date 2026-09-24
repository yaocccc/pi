/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: MIT
 */
/** User-level Pi extension: Action Fusion, ObservationPack, and main-session OCC. */
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { registerActionFusion } from "./extensions/action-fusion/index.ts";
import { registerObservationPack } from "./extensions/observation-pack/index.ts";
import { createOnlineContextCompactExtension } from "./extensions/online-context-compact/extension.ts";

export default function solPiExtension(pi: ExtensionAPI): void {
	registerActionFusion(pi);
	registerObservationPack(pi);
	createOnlineContextCompactExtension()(pi);
}
