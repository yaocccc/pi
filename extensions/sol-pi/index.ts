/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: MIT
 */
/** User-level Pi extension: ObservationPack and main-session OCC. */
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { registerObservationPack } from "./extensions/observation-pack/index.ts";
import { createOnlineContextCompactExtension } from "./extensions/online-context-compact/extension.ts";

export default function solPiExtension(pi: ExtensionAPI): void {
	registerObservationPack(pi);
	createOnlineContextCompactExtension()(pi);
}
