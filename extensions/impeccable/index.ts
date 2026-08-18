import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { Container, Key, matchesKey, type SelectItem, SelectList, Text, truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";
import {
	availableCommandNames,
	buildImpeccablePrompt,
	findImpeccableCommand,
	IMPECCABLE_COMMANDS,
	type ImpeccableCommand,
} from "./commands.ts";

const PANEL_RGB = "16;24;39";
const PANEL_BG = `\x1b[48;2;${PANEL_RGB}m`;
const PANEL_FG = `\x1b[38;2;${PANEL_RGB}m`;
const RESET_BG = "\x1b[49m";
const RESET_FG = "\x1b[39m";

const padToWidth = (text: string, width: number): string => {
	const safeWidth = Math.max(0, width);
	const truncated = truncateToWidth(text, safeWidth, "");
	return truncated + " ".repeat(Math.max(0, safeWidth - visibleWidth(truncated)));
};

const panelBackground = (text: string): string =>
	PANEL_BG + text.replace(/\x1b\[0m/g, `\x1b[0m${PANEL_BG}`) + RESET_BG;

const halfBlockLine = (width: number, position: "top" | "bottom"): string =>
	PANEL_FG + (position === "top" ? "▄" : "▀").repeat(Math.max(0, width)) + RESET_FG;

function insertPrompt(ctx: ExtensionContext, command: ImpeccableCommand): void {
	ctx.ui.setEditorText(buildImpeccablePrompt(command));
	ctx.ui.notify(`已写入 Impeccable 的 ${command.command} 指令，可继续编辑后提交。`, "info");
}

async function showCommandSelector(ctx: ExtensionContext): Promise<ImpeccableCommand | undefined> {
	const items: SelectItem[] = IMPECCABLE_COMMANDS.map((command) => ({
		value: command.command,
		label: command.invocation,
		description: command.description,
	}));

	const selected = await ctx.ui.custom<string | null>((tui, theme, _keybindings, done) => {
		let searchActive = false;
		let searchQuery = "";

		const list = new SelectList(items, Math.min(items.length, 12), {
			selectedPrefix: (text) => theme.fg("accent", text),
			selectedText: (text) => theme.fg("accent", text),
			description: (text) => theme.fg("muted", text),
			scrollInfo: (text) => theme.fg("dim", text),
			noMatch: () => theme.fg("warning", "  没有匹配的指令"),
		});
		list.onSelect = (item) => done(item.value);
		list.onCancel = () => done(null);

		const searchLine = {
			render: (width: number) => [truncateToWidth(
				searchActive ? "  " + theme.fg("accent", "/ ") + theme.fg("text", searchQuery) : " ",
				width,
				"",
			)],
			invalidate: () => {},
		};
		const applySearch = () => list.setFilter(searchQuery.trimStart());

		const container = new Container();
		container.addChild(new Text("", 0, 0));
		container.addChild(new Text(theme.fg("text", theme.bold("选择 Impeccable 设计指令")), 2, 0));
		container.addChild(searchLine);
		container.addChild(list);
		container.addChild(new Text("", 0, 0));
		container.addChild(new Text(theme.fg("dim", "↑↓ 选择 · / 搜索 · Enter 确认 · Esc 清除/取消"), 2, 0));
		container.addChild(new Text("", 0, 0));

		const refresh = () => {
			container.invalidate();
			tui.requestRender();
		};

		return {
			render: (width: number) => [
				halfBlockLine(width, "top"),
				...container.render(width).map((line) => panelBackground(padToWidth(line, width))),
				halfBlockLine(width, "bottom"),
			],
			invalidate: () => container.invalidate(),
			handleInput: (data: string) => {
				if (!searchActive && data === "/") {
					searchActive = true;
					refresh();
					return;
				}

				if (searchActive) {
					if (matchesKey(data, Key.escape)) {
						searchActive = false;
						searchQuery = "";
						applySearch();
						refresh();
						return;
					}
					if (matchesKey(data, Key.backspace)) {
						searchQuery = searchQuery.slice(0, -1);
						applySearch();
						refresh();
						return;
					}
					if (data !== "/" && data.length === 1 && data.charCodeAt(0) >= 32) {
						searchQuery += data;
						applySearch();
						refresh();
						return;
					}
				}

				list.handleInput(data);
				refresh();
			},
		};
	});

	return selected ? findImpeccableCommand(selected) : undefined;
}

export default function impeccableExtension(pi: ExtensionAPI): void {
	pi.registerCommand("impeccable", {
		description: "选择 Impeccable 设计指令并写入可编辑提示词",
		handler: async (args, ctx) => {
		if (ctx.mode !== "tui") {
			ctx.ui.notify("/impeccable 需要交互式 TUI：请选择指令并将模板写入主编辑器。", "warning");
			return;
		}

		const directCommand = args.trim();
		if (directCommand) {
			const command = findImpeccableCommand(directCommand);
			if (!command) {
				ctx.ui.notify(`未知 Impeccable 指令：${directCommand}。可用指令：${availableCommandNames()}`, "error");
				return;
			}
			insertPrompt(ctx, command);
			return;
		}

		const command = await showCommandSelector(ctx);
		if (command) insertPrompt(ctx, command);
		},
	});
}
