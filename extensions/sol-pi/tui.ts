/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: MIT
 */

import type { Theme } from "@earendil-works/pi-coding-agent";
import { type Component, Container, Text } from "@earendil-works/pi-tui";

export type SolPiTuiMechanism =
	| "Action Fusion"
	| "Observation Pack"
	| "Luna Delegating"
	| "Online Context Compact";

const INTEGER_FORMAT = new Intl.NumberFormat("en-US", { maximumFractionDigits: 0 });
export function formatSavingsCount(value: number, unit: string): string {
	const count = Number.isFinite(value) ? Math.max(0, Math.round(value)) : 0;
	return `${INTEGER_FORMAT.format(count)} ${unit}`;
}

export function renderSolPiTool(
	theme: Theme,
	mechanism: SolPiTuiMechanism,
	saving: string,
	base?: Component,
): Component {
	const container = new Container();
	const title = `${theme.fg("warning", "⚡")} ${theme.fg("accent", theme.bold(`SoL-Pi · ${mechanism}`))}`;
	container.addChild(new Text(title, 0, 0));
	container.addChild(new Text(theme.fg("success", `Money saved · ${saving}`), 0, 0));
	if (base) container.addChild(base);
	return container;
}
