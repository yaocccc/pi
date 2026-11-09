import { createHash } from 'node:crypto';
import { AssistantMessageComponent, ToolExecutionComponent, UserMessageComponent, type SessionEntry } from '@earendil-works/pi-coding-agent';
import { Container, stripTerminalSequences, truncateToWidth, type Component, type TuiMouseEvent, type TuiMouseEventResult } from '@earendil-works/pi-tui';
import { groupToolComponents } from './folding.ts';
import { estimateLineBytes, RenderCache } from './render-cache.ts';
import { frameBorder as border, measureWidth as visibleWidth, padToWidth, replaceVisibleContent } from './utils.ts';

type Message = Parameters<AssistantMessageComponent['updateContent']>[0];
type AssistantOptions = ConstructorParameters<typeof AssistantMessageComponent>;
export const ROUND_SUMMARY_ENTRY = 'ui-round-summary';
export type RoundSummaryData = { version: 1; messageId: string; label: string };
// Prototype patches outlive extension reloads, so their views must share the new handler's data.
const SUMMARIES_KEY = Symbol.for('pi.extensions.ui.round-summaries.v1');
const shared = globalThis as typeof globalThis & { [SUMMARIES_KEY]?: Map<string, string> };
const summaries = shared[SUMMARIES_KEY] ??= new Map<string, string>();
const messageKeys = new WeakMap<Message, string>();

// Only finalized messages are keyed. Live messages can mutate in place.
function messageKey(message: Message): string {
    let key = messageKeys.get(message);
    if (!key) {
        key = createHash('sha256').update(JSON.stringify([
            message.timestamp, message.content.filter((part) => part.type === 'text'),
        ])).digest('hex');
        messageKeys.set(message, key);
    }
    return key;
}

export function recordRoundSummary(entries: readonly SessionEntry[], message: Message, label: string): RoundSummaryData | undefined {
    if (message.stopReason !== 'stop' || !message.content.some((part) => part.type === 'text' && part.text.trim())
        || message.content.some((part) => part.type === 'toolCall')) return;
    const key = messageKey(message);
    const entry = entries.filter((entry) => entry.type === 'message' && entry.message.role === 'assistant'
        && messageKey(entry.message) === key).at(-1);
    if (!entry) return;
    summaries.set(key, label);
    return { version: 1, messageId: entry.id, label };
}

export function restoreRoundSummaries(entries: readonly SessionEntry[]): void {
    summaries.clear();
    const messages = new Map(entries.filter((entry) => entry.type === 'message').map((entry) => [entry.id, entry.message]));
    for (const entry of entries) {
        if (entry.type !== 'custom' || entry.customType !== ROUND_SUMMARY_ENTRY) continue;
        const data = entry.data as Partial<RoundSummaryData> | undefined;
        if (data?.version !== 1 || typeof data.messageId !== 'string' || typeof data.label !== 'string') continue;
        const message = messages.get(data.messageId);
        if (message?.role === 'assistant') summaries.set(messageKey(message), data.label);
    }
}

type AssistantState = {
    lastMessage?: Message;
    isStreaming: boolean;
    hideThinkingBlock: boolean;
    markdownTheme: AssistantOptions[2];
    hiddenThinkingLabel: AssistantOptions[3];
    outputPad: AssistantOptions[4];
    markdownTransformers: AssistantOptions[5];
    contentContainer: { children: Component[] };
};
const state = (component: AssistantMessageComponent): AssistantState => component as unknown as AssistantState;
const IMAGE_SEQUENCE = /\x1b(?:_G|\]1337;File=)/;

function trimTrailingTextSpace(lines: string[]): string[] {
    let end = lines.length;
    while (end > 0 && !IMAGE_SEQUENCE.test(lines[end - 1]!) && !stripTerminalSequences(lines[end - 1]!).trim()) end--;
    if (!end) return [];
    // Blank rows following an image are part of its native layout, not text padding.
    if (end === lines.length || IMAGE_SEQUENCE.test(lines[end - 1]!)) return lines;
    const result = lines.slice(0, end);
    const closingMarkers = lines.slice(end).map(line => replaceVisibleContent(line, '')).join('');
    const last = result[end - 1]!;
    const markers = replaceVisibleContent(last, '');
    result[end - 1] = markers + closingMarkers + last.slice(markers.length);
    return result;
}

// Render a filtered view, never edit lastMessage or the original content tree.
// Thinking is always excluded; native text rendering and mouse handling stay intact.
class AssistantView implements Component {
    private display?: AssistantMessageComponent;
    private signature: unknown[] = [];
    private configuration: unknown[] = [];

    constructor(private source: AssistantMessageComponent) {}

    render(width: number): string[] {
        const source = state(this.source);
        if (!source.lastMessage) return [];
        const signature = [source.lastMessage, source.contentContainer.children, source.isStreaming];
        const configuration = [source.hideThinkingBlock, source.markdownTheme, source.hiddenThinkingLabel,
            source.outputPad, source.markdownTransformers,
            (globalThis as any)[Symbol.for('@earendil-works/pi-coding-agent:theme')]];
        if (!this.display || configuration.some((value, index) => value !== this.configuration[index])) {
            this.display = new AssistantMessageComponent(undefined, source.hideThinkingBlock, source.markdownTheme,
                source.hiddenThinkingLabel, source.outputPad, source.markdownTransformers);
            this.configuration = configuration;
            this.signature = [];
        }
        if (signature.some((value, index) => value !== this.signature[index])) {
            const message: Message = {
                ...source.lastMessage,
                content: source.lastMessage.content.filter((part) => part.type !== 'thinking'),
            };
            // Keep native Markdown block caches across streaming updates (Pi 1.5+).
            this.display.updateContent(message, source.isStreaming);
            this.signature = signature;
        }
        return trimTrailingTextSpace(this.display.render(width));
    }

    invalidate(): void { this.display = undefined; }
    handleMouse(event: TuiMouseEvent): TuiMouseEventResult | undefined { return this.display?.handleMouse(event); }
}

const views = new WeakMap<AssistantMessageComponent, AssistantView>();
function view(component: AssistantMessageComponent): AssistantView {
    let entry = views.get(component);
    if (!entry) {
        entry = new AssistantView(component);
        views.set(component, entry);
    }
    return entry;
}

function summaryFor(component: AssistantMessageComponent): string | undefined {
    const source = state(component);
    return !source.isStreaming && source.lastMessage?.stopReason === 'stop'
        ? summaries.get(messageKey(source.lastMessage)) : undefined;
}

export class RoundFrame implements Component {
    private label = '';
    private body = new Container();
    private contentHeight = 0;
    private contentOffset = 0;
    private inset = 0;
    private frameWidth = 0;
    private nativeWidth = 0;
    private cache = new RenderCache<{ width: number; label: string; content: string[]; lines: string[] }>();

    update(label: string, children: Component[]): void {
        this.label = stripTerminalSequences(label).replace(/\s+/g, ' ');
        this.body.children = children;
    }

    render(width: number): string[] {
        if (width <= 0) {
            this.cache.clear();
            this.contentHeight = this.contentOffset = this.frameWidth = 0;
            return [];
        }
        // Always render children: streaming/tool state and native mouse layouts may
        // change even when the projected children or their visible text do not.
        this.inset = width >= 8 ? 1 : 0;
        this.nativeWidth = Math.max(3, width - this.inset * 2);
        const content = this.body.render(this.nativeWidth);
        const cached = this.cache.get();
        // The frame depends only on width, label and exact rendered strings (including
        // theme ANSI, semantic markers and image protocols), not child identities.
        if (cached && cached.width === width && cached.label === this.label
            && cached.content.length === content.length
            && content.every((line, index) => line === cached.content[index])) return cached.lines.slice();
        const lines = this.renderFrame(width, content);
        // Keep only the latest snapshot; streaming updates must not accumulate versions.
        this.cache.set({ width, label: this.label, content: content.slice(), lines },
            estimateLineBytes(content) + estimateLineBytes(lines));
        // Parent layout code may splice/replace rows; never expose the cached array.
        return lines.slice();
    }

    private renderFrame(width: number, raw: string[]): string[] {
        this.contentHeight = this.contentOffset = this.frameWidth = 0;
        let content = trimTrailingTextSpace(raw);
        this.contentHeight = content.length;
        if (!content.length) return [];
        // Image renderers can reserve leading blank rows before placing the image.
        const firstVisible = content.find(line => IMAGE_SEQUENCE.test(line) || stripTerminalSequences(line).trim()) ?? '';
        // Replace only a leading text spacer, preserving semantic zone markers.
        if (content.length > 1 && !IMAGE_SEQUENCE.test(firstVisible) && !stripTerminalSequences(content[0]).trim()) {
            this.contentOffset = 1;
            content = [replaceVisibleContent(content[0], content[1]), ...content.slice(2)];
        }
        const contentWidth = content.reduce((max, line) => Math.max(max, visibleWidth(stripTerminalSequences(line).trimEnd())), 0);
        this.frameWidth = !this.inset || content.some(line => IMAGE_SEQUENCE.test(line)) ? width
            : Math.min(width, Math.max(8, visibleWidth(this.label) + 6, contentWidth + 3));
        const innerWidth = this.frameWidth - this.inset * 2;
        const title = truncateToWidth(this.label, this.inset ? this.frameWidth - 6 : width);
        if (!this.inset) return [padToWidth(title, width), ...content.map(line => padToWidth(line, width))];
        const header = border('╭', 'topLeft') + border('─ ', 'top') + title
            + border(` ${'─'.repeat(this.frameWidth - visibleWidth(title) - 5)}`, 'top') + border('╮', 'topRight');
        const left = border('│', 'left');
        const right = border('│', 'right');
        return [
            header,
            ...content.map(line => {
                const markers = replaceVisibleContent(line, '');
                return markers + left + padToWidth(line.slice(markers.length), innerWidth) + right;
            }),
            border('╰', 'bottomLeft') + border('─'.repeat(innerWidth), 'bottom') + border('╯', 'bottomRight'),
        ];
    }

    invalidate(): void {
        this.cache.clear();
        this.body.invalidate();
    }
    handleMouse(event: TuiMouseEvent): TuiMouseEventResult | undefined {
        if (event.y < 1 || event.x < this.inset || event.x >= this.frameWidth - this.inset) return;
        const y = event.y - 1 + this.contentOffset;
        if (y < this.contentHeight) {
            return this.body.handleMouse({ ...event, x: event.x - this.inset, y,
                width: this.nativeWidth, height: this.contentHeight });
        }
    }
}

const roundViews = new WeakMap<Component, RoundFrame>();

function layoutRound(children: Component[]): Component[] {
    const first = children.findIndex((child) => child instanceof AssistantMessageComponent || child instanceof ToolExecutionComponent);
    if (first < 0) return children;
    let title = '助手';
    const notices = new Set<Component>();
    const commentary = new Set<Component>();
    const projected = children.map((child) => {
        if (!(child instanceof AssistantMessageComponent)) return child;
        const message = state(child).lastMessage;
        if (message?.model) title = [message.provider, message.model].filter(Boolean).join(' · ');
        const entry = view(child);
        if (message?.content.some((part) => part.type === 'text' && part.text.trim())) {
            commentary.add(entry);
        }
        if (message && ['error', 'aborted', 'length'].includes(message.stopReason)) notices.add(entry);
        return entry;
    });
    // Explanations stay in transcript order, outside the following tool group.
    // Empty carriers and hidden thinking do not split otherwise adjacent calls.
    const result = projected.slice(0, first);
    let activity: Component[] = [];
    const flush = () => {
        result.push(...groupToolComponents(activity, activity.filter((component) => notices.has(component)).length));
        activity = [];
    };
    for (const component of projected.slice(first)) {
        if (commentary.has(component)) {
            flush();
            result.push(component);
        } else activity.push(component);
    }
    flush();
    // Only transcript rounds get a frame; standalone tool containers stay native groups.
    if (children.some(child => child instanceof UserMessageComponent || child instanceof AssistantMessageComponent)) {
        let frame = roundViews.get(children[first]!);
        if (!frame) {
            frame = new RoundFrame();
            roundViews.set(children[first]!, frame);
        }
        const keep = children.findIndex(child => child instanceof UserMessageComponent) + 1;
        frame.update(title, result.splice(keep));
        result.push(frame);
    }
    return result;
}

export function layoutRounds(children: Component[]): Component[] {
    const rounds: Component[][] = [];
    let current: Component[] = [];
    for (const child of children) {
        if (child instanceof UserMessageComponent && current.length) {
            rounds.push(current);
            current = [];
        }
        current.push(child);
        // A saved conclusion also ends a round (e.g. extension follow-ups without a user card).
        if (child instanceof AssistantMessageComponent && summaryFor(child)) {
            rounds.push(current);
            current = [];
        }
    }
    if (current.length) rounds.push(current);
    return rounds.flatMap(layoutRound);
}
