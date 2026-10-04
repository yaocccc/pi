/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: MIT
 */

import type { Component } from "@earendil-works/pi-tui";

/** Background tools retain model-visible results without adding terminal cards. */
export function hiddenRenderer(): Component {
	return {
		render: (_width) => [],
		invalidate() {},
	};
}
