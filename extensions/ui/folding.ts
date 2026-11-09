import { createEditToolDefinition, ToolExecutionComponent, type Theme } from '@earendil-works/pi-coding-agent';
import { stripTerminalSequences, truncateToWidth, visibleWidth, type Component, type TuiMouseEvent, type TuiMouseEventResult } from '@earendil-works/pi-tui';
import { padToWidth } from './utils.ts';
import { releaseToolCache } from './render-cache.ts';
import { colorToolTokens, estimateToolTokens, formatToolTokens } from './tool-tokens.ts';
import { getWorkerRenderSnapshot } from '../worker/presentation.ts';
import type { WorkerUiDetails, WorkerUiStatus, WorkerUsage } from '../worker/types.ts';
import { sanitizeUiText, uiDuration, workerUsageText } from '../worker/ui.ts';

const FOLDING_PATCH = Symbol.for('pi.extensions.ui.tool-folding.v30');
const THEME_KEY = Symbol.for('@earendil-works/pi-coding-agent:theme');
type Tool = ToolExecutionComponent & Component;
type ToolResult = Parameters<typeof estimateToolTokens>[1] & { details?: unknown };
type ToolState = {
    toolName: string;
    args?: Record<string, unknown>;
    expanded: boolean;
    isPartial: boolean;
    hideComponent?: boolean;
    result?: ToolResult;
    imageComponents?: unknown[];
    rendererState?: Record<string, unknown>;
    toolDefinition?: { renderCall?: unknown; renderResult?: unknown };
    getRenderShell?(): string;
    updateDisplay(): void;
    ui?: { requestRender(): void };
};
const state = (tool: Tool): ToolState => tool as unknown as ToolState;
const nativeEdit = createEditToolDefinition(process.cwd());
// Native edit always draws a call header, even for an empty/error result.
// Match both functions, not the name: extensions may override a tool named edit.
export function hasNativeEditRenderer(tool: Tool): boolean {
    const definition = state(tool).toolDefinition;
    return definition?.renderCall === nativeEdit.renderCall && definition?.renderResult === nativeEdit.renderResult;
}
const groups = new WeakMap<Component, ToolActivityGroup>();
const membership = new WeakMap<Tool, ToolActivityGroup>();
const previews = new WeakSet<Tool>();
const tokenCache = new WeakMap<Tool, { args: unknown; result: ToolResult; tokens: number }>();
let localToggle = false;
let cyclePreview = false;
const theme = (): Pick<Theme, 'fg'> | undefined => (globalThis as any)[THEME_KEY];
const fg = (color: Parameters<Theme['fg']>[0], text: string): string => theme()?.fg(color, text) ?? text;
const clean = (text: string): string => stripTerminalSequences(text).replace(/\s+/g, ' ').replace(/[\x00-\x1f\x7f-\x9f]/g, '').trim();
const running = (tool: Tool): boolean => state(tool).isPartial || !state(tool).result;
const fit = (text: string, width: number): string => padToWidth(truncateToWidth(text, width, '…'), width);

function tokens(tool: Tool): number {
    const { args, result } = state(tool);
    if (!result || running(tool)) return 0;
    let cached = tokenCache.get(tool);
    if (!cached || cached.args !== args || cached.result !== result) {
        cached = { args, result, tokens: estimateToolTokens(args, result) };
        tokenCache.set(tool, cached);
    }
    return cached.tokens;
}

type BadgeCache = { current?: {
    text: string; width: number; badge: string; painted: string; suffix: string; inline: boolean; line: string;
} };

function withBadge(cache: BadgeCache, text: string, count: number, width: number, minWidth: number, suffix = '', inline = false): string {
    const badge = count && width >= minWidth ? formatToolTokens(count) : '';
    const painted = badge && theme() ? colorToolTokens(count, badge, theme()!) : badge;
    const previous = cache.current;
    // Cache only formatting, after resolving all dynamic text and badge colors.
    // Exact strings also catch theme changes without relying on theme identity.
    if (previous && previous.text === text && previous.width === width && previous.badge === badge
        && previous.painted === painted && previous.suffix === suffix && previous.inline === inline) return previous.line;
    const gap = inline ? ' ' : '  ';
    const textWidth = width - (badge ? visibleWidth(badge) + gap.length : 0);
    const headroom = textWidth - visibleWidth(suffix);
    // Keep status adjacent to the text, padding only after the entire summary.
    // On narrow screens shorten the breakdown before dropping status.
    const body = suffix && headroom >= 2 ? truncateToWidth(text, headroom, '…') + suffix : text;
    const line = !badge ? fit(body, width)
        : fit((inline ? truncateToWidth(body, textWidth, '…') : fit(body, textWidth)) + gap + painted, width);
    cache.current = { text, width, badge, painted, suffix, inline, line };
    return line;
}

function callLabel(tool: Tool): string {
    const { toolName, args } = state(tool);
    let value = args?.path ?? args?.file_path ?? args?.command ?? args?.query ?? args?.pattern ?? args?.url ?? args?.code;
    if (Array.isArray(args?.tasks)) {
        const tasks = args.tasks as Array<{ objective?: unknown }>;
        value = `${tasks.length} tasks${typeof tasks[0]?.objective === 'string' ? ` · ${tasks[0].objective}` : ''}`;
    }
    const previewArgs = typeof value === 'string' ? clean(value.slice(0, 240)) + (value.length > 240 ? '…' : '') : '';
    return clean(toolName) + (previewArgs ? `  ${fg('dim', previewArgs)}` : '');
}

type TaskPreview = {
    index: number; objective: string; mode?: string; status: WorkerUiStatus;
    requestedPreset?: string; resolvedPreset?: string; sessionMode?: string;
    usage?: WorkerUsage; startedAt?: number; finishedAt?: number;
};

function workerTasks(tool: Tool): { tasks: TaskPreview[]; now: number } {
    const value = state(tool);
    const details = (getWorkerRenderSnapshot(value.rendererState) ?? value.result?.details) as Partial<WorkerUiDetails> & { status?: WorkerUiStatus } | undefined;
    // Live snapshots refresh with the Worker clock; historical snapshots stay frozen.
    const now = details?.snapshotAt ?? details?.finishedAt ?? (running(tool) ? Date.now() : details?.startedAt ?? Date.now());
    if (details?.kind === 'worker-ui' && Array.isArray(details.tasks)) return { tasks: details.tasks, now };
    if (!Array.isArray(value.args?.tasks)) return { tasks: [], now };
    const status = details?.status === 'failed' || details?.status === 'blocked' ? details.status
        : running(tool) ? 'queued' : value.result?.isError ? 'failed' : 'completed';
    return { now, tasks: value.args.tasks.map((task, index) => ({
        index, status,
        objective: typeof task?.objective === 'string' ? task.objective : '',
        mode: typeof task?.mode === 'string' ? task.mode : undefined,
        requestedPreset: typeof task?.preset === 'string' ? task.preset : undefined,
        sessionMode: typeof task?.sessionMode === 'string' ? task.sessionMode : undefined,
    })) };
}

function taskStatus(status: WorkerUiStatus): string {
    switch (status) {
        case 'queued': return fg('muted', '○');
        case 'running': return fg('warning', '⋯');
        case 'completed': return fg('success', '✓');
        case 'blocked': return fg('warning', '!');
        case 'failed': return fg('error', '✗');
    }
}

function resultPreview(tool: Tool): string {
    const text = state(tool).result?.content.find((part) => part.type === 'text');
    return text?.type === 'text' ? clean(text.text.slice(0, 240)) : '';
}

type Hit = { start: number; height: number; component: Component; toggle?: Tool; indent: number; width: number };

export class ToolActivityGroup implements Component {
    children: Component[] = [];
    open = false;
    private hits: Hit[] = [];
    private header = false;
    private notices = 0;
    private summaryBadge: BadgeCache = {};
    private previewBadges = new WeakMap<Tool, BadgeCache>();
    private previewLayouts = new WeakMap<Tool, { width: number; lines: string[]; clipped: boolean }>();

    update(children: Component[], notices: number): void {
        this.children = children;
        this.notices = notices;
        for (const child of children) {
            if (child instanceof ToolExecutionComponent) membership.set(child, this);
        }
    }

    private tools(): Tool[] {
        return this.children.filter((child): child is Tool => child instanceof ToolExecutionComponent);
    }

    private requestRender(): void {
        const tool = this.tools()[0];
        if (tool) state(tool).ui?.requestRender();
    }

    invalidate(): void {
        this.summaryBadge.current = undefined;
        this.previewBadges = new WeakMap();
        for (const child of this.children) child.invalidate();
    }

    render(width: number): string[] {
        this.hits = [];
        this.previewLayouts = new WeakMap();
        const lines: string[] = [];
        // Native edit always has a header: do not render its whole diff just to
        // decide visibility. Custom self renderers still need a live probe (e.g.
        // Worker control calls can render zero lines without updateDisplay).
        const visible = this.tools().filter((tool) => !state(tool).hideComponent
            && (state(tool).getRenderShell?.() !== 'self' || hasNativeEditRenderer(tool) || tool.render(width).length > 0));
        this.header = visible.length > 0;
        if (this.header) {
            const counts = new Map<string, number>();
            for (const tool of visible) {
                const name = clean(state(tool).toolName);
                counts.set(name, (counts.get(name) ?? 0) + 1);
            }
            const active = visible.filter(running).length;
            const failed = visible.filter((tool) => state(tool).result?.isError).length + this.notices;
            const title = `${visible.length} ${visible.length === 1 ? 'call' : 'calls'}`;
            const lead = `${this.open ? '⌄' : '›'} ${title}`;
            const breakdown = [...counts].map(([name, count]) => `${name}×${count}`).join('  ');
            const content = `${lead}${fg('muted', ' · ')}${fg('dim', breakdown)}`;
            const totalTokens = visible.reduce((sum, tool) => sum + tokens(tool), 0);
            const compact = width < 48;
            const statuses = [
                ...(active ? [fg('warning', compact ? `⋯${active}` : `⋯ ${active} running`)] : []),
                ...(failed ? [fg('error', compact ? `✗${failed}` : `✗ ${failed} failed`)] : []),
            ];
            const suffix = statuses.length ? '  ' + statuses.join('  ') : '';
            const summaryInset = width > 0 ? ' ' : '';
            lines.push(summaryInset + withBadge(this.summaryBadge, content, totalTokens, width - summaryInset.length, 24, suffix, true));
        }
        const callInset = width > 5 ? '   ' : '';
        const append = (component: Component, body: string[], toggle?: Tool, indent = 0) => {
            this.hits.push({ start: lines.length, height: body.length, component, toggle, indent, width: width - indent });
            const gutter = ' '.repeat(indent);
            lines.push(...(indent ? body.map((line) => gutter + line) : body));
        };
        // A running Worker exposes every task even while its group is folded.
        // Keep explicitly opened Worker details until the user closes them.
        const shown = this.open ? visible : visible.filter((tool) => state(tool).toolName === 'worker'
            && (running(tool) || state(tool).expanded));
        const shownSet = new Set(shown);
        for (const child of this.children) {
            if (!(child instanceof ToolExecutionComponent)) {
                if (this.open || !this.header) append(child, child.render(width));
                continue;
            }
            if (!shownSet.has(child)) { releaseToolCache(child); continue; }
            const value = state(child);
            const arrow = value.expanded ? '⌄' : '›';
            const preview = value.toolName === 'worker' ? workerTasks(child) : undefined;
            if (preview?.tasks.length) {
                for (const task of preview.tasks) {
                    const mode = clean(task.mode ?? `task ${task.index + 1}`);
                    const sessionIcon = task.sessionMode === 'fork' ? '' : task.sessionMode === 'new' ? '󰐖' : '';
                    const usage = task.usage ? workerUsageText(task.usage, task.status === 'running') : '';
                    const runtime = [task.resolvedPreset ?? task.requestedPreset, usage, uiDuration(task, preview.now)]
                        .filter(Boolean).map((part) => clean(part!)).join(' · ');
                    const objective = clean(sanitizeUiText(task.objective)).slice(0, 240);
                    const row = `${callInset}${arrow} ${taskStatus(task.status)} ${mode}${sessionIcon ? fg('muted', ` ${sessionIcon}`) : ''}`
                        + (runtime ? fg('muted', ` · ${runtime}`) : '') + (objective ? fg('dim', ` · ${objective}`) : '');
                    append(child, [fit(row, width)], child);
                }
            } else {
                const status = value.result?.isError ? fg('error', '✗') : running(child) ? fg('warning', '⋯') : fg('success', '✓');
                const preview = resultPreview(child);
                const row = `${callInset}${arrow} ${status} ${callLabel(child)}${preview ? fg(value.result?.isError ? 'error' : 'dim', ` · ${preview}`) : ''}`;
                let cache = this.previewBadges.get(child);
                if (!cache) {
                    cache = {};
                    this.previewBadges.set(child, cache);
                }
                append(child, [withBadge(cache, row, tokens(child), width, 80)], child);
            }
            if (!value.expanded) { releaseToolCache(child); continue; }
            // Framed output shares the call-row baseline; no extra tree rail.
            // Other self-renderers and graphics retain their native width.
            const framed = value.toolName === 'worker' || hasNativeEditRenderer(child) || value.getRenderShell?.() !== 'self';
            const indent = framed && !value.imageComponents?.length ? callInset.length : 0;
            const body = child.render(width - indent);
            const clipped = previews.has(child) && !value.imageComponents?.length && body.length > 12;
            if (previews.has(child)) this.previewLayouts.set(child, { width: width - indent, lines: clipped ? [] : body.slice(), clipped });
            const footer = clipped && /^╭─.*╮$/.test(stripTerminalSequences(body[0]!))
                && /^╰─*╯$/.test(stripTerminalSequences(body.at(-1)!)) ? body.at(-1) : undefined;
            append(child, clipped ? body.slice(0, footer ? 11 : 12) : body, undefined, indent);
            // A retained bottom edge is display-only, not a hit target for a hidden output row.
            if (footer) lines.push(' '.repeat(indent) + footer);
            if (clipped) lines.push(' '.repeat(indent)
                + fit(fg('dim', '… 再次点击或 Ctrl+O 查看完整输出'), width - indent));
        }
        return lines;
    }

    handleMouse(event: TuiMouseEvent): TuiMouseEventResult | undefined {
        const click = event.type === 'click' && event.button === 'left';
        if (this.header && event.y === 0 && click) {
            this.open = !this.open;
            if (!this.open) this.locally(() => this.tools().forEach((tool) => tool.setExpanded(false)));
            this.requestRender();
            return { handled: true };
        }
        const hit = this.hits.find(({ start, height }) => event.y >= start && event.y < start + height);
        if (!hit) return undefined;
        if (hit.toggle) {
            if (!click) return undefined;
            this.locally(() => hit.toggle!.setExpanded(!state(hit.toggle!).expanded), true);
            this.requestRender();
            return { handled: true };
        }
        if (event.x < hit.indent) return undefined;
        return this.locally(() => hit.component.handleMouse?.({
            ...event, x: event.x - hit.indent, y: event.y - hit.start, height: hit.height, width: hit.width,
        }), click);
    }

    expandPreview(tool: Tool): boolean {
        const preview = this.previewLayouts.get(tool);
        if (!preview) return false;
        previews.delete(tool);
        state(tool).updateDisplay();
        // Only probe full output on the second click, never on input or scroll redraws.
        if (preview.clipped) return true;
        const full = tool.render(preview.width);
        return full.length !== preview.lines.length || full.some((line, index) => line !== preview.lines[index]);
    }

    private locally<T>(action: () => T, cycle = false): T {
        const previousToggle = localToggle;
        const previousCycle = cyclePreview;
        localToggle = true;
        cyclePreview = cycle;
        try { return action(); }
        finally { localToggle = previousToggle; cyclePreview = previousCycle; }
    }
}

// The round planner supplies the activity between explanations, without transcript mutation.
export function groupToolComponents(children: Component[], notices = 0): Component[] {
    const firstTool = children.find((child): child is Tool => child instanceof ToolExecutionComponent);
    if (!firstTool) return children;
    let group = groups.get(firstTool);
    if (!group) {
        group = new ToolActivityGroup();
        group.open = state(firstTool).expanded;
        groups.set(firstTool, group);
    }
    group.update(children, notices);
    return [group];
}

export function patchToolFolding(): void {
    const prototype = ToolExecutionComponent.prototype as unknown as {
        setExpanded(this: Tool, expanded: boolean): void;
        updateDisplay(this: Tool): void;
        [FOLDING_PATCH]?: boolean;
    };
    if (prototype[FOLDING_PATCH]) return;
    const original = prototype.setExpanded;
    const updateDisplay = prototype.updateDisplay;
    prototype.setExpanded = function (expanded: boolean): void {
        if (cyclePreview && !expanded && previews.has(this) && membership.get(this)?.expandPreview(this)) return;
        if (localToggle && expanded) previews.add(this);
        else previews.delete(this);
        original.call(this, expanded);
        if (!expanded) releaseToolCache(this);
        if (!localToggle) {
            const group = membership.get(this);
            if (group) group.open = expanded;
        }
    };
    prototype.updateDisplay = function (): void {
        const value = state(this);
        const expanded = value.expanded;
        // First click uses the native compact renderer; second click (or Ctrl+O) requests full output.
        if (previews.has(this)) value.expanded = false;
        try { updateDisplay.call(this); }
        finally { value.expanded = expanded; }
    };
    prototype[FOLDING_PATCH] = true;
}
