import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { join } from "node:path";
import {
	CustomEditor,
	type ExtensionCommandContext,
	getAgentDir,
	type KeybindingsManager,
	type Theme,
} from "@earendil-works/pi-coding-agent";
import {
	CombinedAutocompleteProvider,
	type EditorTheme,
	Key,
	matchesKey,
	truncateToWidth,
	type TUI,
	visibleWidth,
} from "@earendil-works/pi-tui";
import type { AgentDefinition, AgentName } from "./types/index.ts";

const TEXTAREA_RGB = "16;24;39";
const TEXTAREA_BG = `\x1b[48;2;${TEXTAREA_RGB}m`;
const TEXTAREA_FG = `\x1b[38;2;${TEXTAREA_RGB}m`;
const RESET_BG = "\x1b[49m";
const RESET_FG = "\x1b[39m";

const padToWidth = (text: string, width: number): string => {
	const safeWidth = Math.max(0, width);
	const truncated = truncateToWidth(text, safeWidth, "");
	return truncated + " ".repeat(Math.max(0, safeWidth - visibleWidth(truncated)));
};

const textAreaBg = (text: string): string =>
	TEXTAREA_BG + text.replace(/\x1b\[0m/g, `\x1b[0m${TEXTAREA_BG}`) + RESET_BG;

const halfBlockLine = (width: number, position: "top" | "bottom"): string =>
	TEXTAREA_FG + (position === "top" ? "▄" : "▀").repeat(Math.max(0, width)) + RESET_FG;

let cachedFdPath: string | null | undefined;

function getFdPath(): string | null {
	if (cachedFdPath !== undefined) return cachedFdPath;
	const localFd = join(getAgentDir(), "bin", process.platform === "win32" ? "fd.exe" : "fd");
	if (existsSync(localFd)) {
		cachedFdPath = localFd;
		return cachedFdPath;
	}
	try {
		const output = execFileSync("sh", ["-lc", "command -v fd || command -v fdfind || true"], {
			encoding: "utf8",
			stdio: ["ignore", "pipe", "ignore"],
		}).trim();
		cachedFdPath = output || null;
	} catch {
		cachedFdPath = null;
	}
	return cachedFdPath;
}

function editorTheme(theme: Theme): EditorTheme {
	return {
		borderColor: (text) => theme.fg("borderMuted", text),
		selectList: {
			selectedPrefix: (text) => theme.fg("accent", text),
			selectedText: (text) => theme.fg("accent", text),
			description: (text) => theme.fg("muted", text),
			scrollInfo: (text) => theme.fg("dim", text),
			noMatch: (text) => theme.fg("warning", text),
		},
	};
}

class SubagentTextAreaEditor extends CustomEditor {
	private readonly background = textAreaBg;
	private readonly blankBorder = (text: string) => this.background(" ".repeat(visibleWidth(text)));
	private readonly done: (value: string | undefined) => void;
	private readonly hint: string;
	private closed = false;

	constructor(
		tui: TUI,
		theme: EditorTheme,
		keybindings: KeybindingsManager,
		done: (value: string | undefined) => void,
		hint: string,
	) {
		super(tui, { ...theme, borderColor: (text) => this.blankBorder(text) }, keybindings, { paddingX: 1 });
		this.done = done;
		this.hint = hint;
		this.onSubmit = (text) => this.close(text.trim() ? text : undefined);
		this.onEscape = () => this.close(undefined);
	}

	override render(width: number): string[] {
		const previousBorderColor = this.borderColor;
		this.borderColor = this.blankBorder;
		try {
			const lines = super.render(width);
			const showingAutocomplete = this.isShowingAutocomplete();
			const textarea = lines.map((line, index) => {
				if (index === 0) return halfBlockLine(width, "top");
				if (!showingAutocomplete && index === lines.length - 1) return halfBlockLine(width, "bottom");
				return this.background(padToWidth(line, width));
			});
			if (showingAutocomplete) textarea.push(halfBlockLine(width, "bottom"));
			return [padToWidth(this.hint, width), ...textarea];
		} finally {
			this.borderColor = previousBorderColor;
		}
	}

	private close(value: string | undefined): void {
		if (this.closed) return;
		this.closed = true;
		this.done(value);
	}
}

export function readSubagentText(ctx: ExtensionCommandContext, hint: string): Promise<string | undefined> {
	return ctx.ui.custom<string | undefined>((tui, theme, keybindings, done) => {
		const editor = new SubagentTextAreaEditor(tui, editorTheme(theme), keybindings, done, hint);
		editor.setAutocompleteProvider(new CombinedAutocompleteProvider([], ctx.cwd, getFdPath()));
		return editor;
	});
}

function renderAgentLine(agent: AgentDefinition, selected: boolean, theme: Theme): string {
	const prefix = selected ? theme.fg("accent", "> ") : "  ";
	const name = theme.fg(selected ? "accent" : "text", agent.name.padEnd(10));
	const description = theme.fg(selected ? "text" : "muted", agent.description);
	return `${prefix}${name}${description}`;
}

export async function selectAgent(
	ctx: ExtensionCommandContext,
	agents: AgentDefinition[],
): Promise<AgentName | undefined> {
	return ctx.ui.custom<AgentName | undefined>((tui, theme, _keybindings, done) => {
		let selectedIndex = 0;
		let cachedLines: string[] | undefined;

		const refresh = () => {
			cachedLines = undefined;
			tui.requestRender();
		};

		const handleInput = (data: string) => {
			if (matchesKey(data, Key.up)) {
				selectedIndex = Math.max(0, selectedIndex - 1);
				refresh();
				return;
			}
			if (matchesKey(data, Key.down)) {
				selectedIndex = Math.min(agents.length - 1, selectedIndex + 1);
				refresh();
				return;
			}
			if (matchesKey(data, Key.enter) || matchesKey(data, Key.space)) {
				done(agents[selectedIndex]?.name);
				return;
			}
			if (matchesKey(data, Key.escape)) done(undefined);
		};

		const addLine = (lines: string[], width: number, text = "") =>
			lines.push(textAreaBg(padToWidth(text, width)));

		const render = (width: number): string[] => {
			if (cachedLines) return cachedLines;

			const lines: string[] = [];
			lines.push(halfBlockLine(width, "top"));
			addLine(lines, width, theme.fg("accent", theme.bold(" 选择子代理")));
			addLine(lines, width, theme.fg("dim", " 选择角色后输入任务并立即启动"));
			addLine(lines, width);

			for (let index = 0; index < agents.length; index++) {
				const agent = agents[index]!;
				addLine(lines, width, renderAgentLine(agent, index === selectedIndex, theme));
			}

			addLine(lines, width);
			addLine(lines, width, theme.fg("dim", " ↑↓ 选择 • Enter 确认 • Esc 取消"));
			lines.push(halfBlockLine(width, "bottom"));

			cachedLines = lines;
			return lines;
		};

		return {
			render,
			invalidate: () => {
				cachedLines = undefined;
			},
			handleInput,
		};
	});
}
