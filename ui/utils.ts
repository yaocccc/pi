import { stripTerminalSequences, truncateToWidth, visibleWidth } from '@earendil-works/pi-tui';

// These characters cannot join into wider/zero-width graphemes. Keep every
// other character (including tabs and combining marks) on the native path.
const singleCellText = /^[\x20-\x7e\u2500-\u257f]*$/;
const singleCellWidth = (text: string): number => {
    const plain = stripTerminalSequences(text);
    return singleCellText.test(plain) ? plain.length : -1;
};

export const measureWidth = (text: string): number => {
    const width = singleCellWidth(text);
    return width < 0 ? visibleWidth(text) : width;
};

const FRAME_COLORS = {
    user: {
        topLeft: '#68d3ae', top: '#6ed8b4', topRight: '#79dcbc', right: '#87dec3',
        bottomRight: '#7bd4b8', bottom: '#6ccdaf', bottomLeft: '#5fc6a5', left: '#62ccaa',
    },
    assistant: {
        topLeft: '#78b5ef', top: '#80bff5', topRight: '#8ac7f6', right: '#91c8ef',
        bottomRight: '#85bbe8', bottom: '#77afe0', bottomLeft: '#6ea6dc', left: '#71afe6',
    },
    tool: {
        topLeft: '#71899f', top: '#71899f', topRight: '#71899f', right: '#71899f',
        bottomRight: '#71899f', bottom: '#71899f', bottomLeft: '#71899f', left: '#71899f',
    },
    input: {
        topLeft: '#b6a0e8', top: '#bca8ee', topRight: '#c4b0f2', right: '#c8b5ef',
        bottomRight: '#bda9e7', bottom: '#b19ddd', bottomLeft: '#a892d8', left: '#ad97df',
    },
} as const;
export type FrameKind = keyof typeof FRAME_COLORS;
export type FrameEdge = keyof typeof FRAME_COLORS.assistant;
const frameAnsi = Object.fromEntries(Object.entries(FRAME_COLORS).map(([kind, colors]) => [kind,
    Object.fromEntries(Object.entries(colors).map(([edge, color]) => [edge,
        `\x1b[38;2;${[1, 3, 5].map(offset => parseInt(color.slice(offset, offset + 2), 16)).join(';')}m`,
    ])),
])) as Record<FrameKind, Record<FrameEdge, string>>;

export const frameBorder = (text: string, edge: FrameEdge, kind: FrameKind = 'assistant'): string => frameAnsi[kind][edge] + text + '\x1b[39m';

export const toolFrameTop = (title: string, width: number): string => {
    const label = width >= 6 ? truncateToWidth(title, width - 6, '') : '';
    const middle = label ? frameBorder('─ ', 'top', 'tool') + label + frameBorder(' ' + '─'.repeat(width - measureWidth(label) - 5), 'top', 'tool')
        : frameBorder('─'.repeat(Math.max(0, width - 2)), 'top', 'tool');
    return frameBorder('╭', 'topLeft', 'tool') + middle + frameBorder('╮', 'topRight', 'tool');
};

export const formatTokenCount = (value: number | undefined): string => !Number.isFinite(value) ? '0' : value! >= 1_000_000 ? `${(value! / 1_000_000).toFixed(1)}M` : value! >= 1_000 ? `${Math.round(value! / 1_000)}k` : String(value);

export const formatElapsed = (ms: number): string => {
    const totalSeconds = Math.max(0, Math.floor(ms / 1000));
    const minutes = Math.floor(totalSeconds / 60);
    const seconds = totalSeconds % 60;
    return minutes > 0 ? `${minutes}m${seconds}s` : `${seconds}s`;
};

// These common CJK characters also remain independent across ANSI boundaries.
// Keep combining characters, emoji, tabs and other complex text on the native path.
const independentText = /^[\x20-\x7e\u2010-\u2027\u2500-\u257f\u4e00-\u9fff]*$/;

export const padToWidth = (text: string, width: number): string => {
    if (width <= 0) return '';
    const plain = stripTerminalSequences(text);
    const simple = singleCellText.test(plain);
    const measured = simple ? plain.length : visibleWidth(text);
    if (measured <= width && (simple || !/[\x1b\t]/.test(text) || independentText.test(plain))) {
        return text + ' '.repeat(width - measured);
    }
    // Only independently single-cell text with standard SGR escapes may take
    // this path. OSC links, images, controls and complex graphemes stay native.
    if (simple && measured > width && Number.isFinite(width) && !/\x1b(?!\[[0-9;:]*m)/.test(text)) {
        let index = 0;
        let remaining = Math.floor(width);
        while (remaining > 0) {
            if (text.charCodeAt(index) === 0x1b) index = text.indexOf('m', index) + 1;
            else { index++; remaining--; }
        }
        // Native truncation drops pending ANSI after the final retained cell,
        // then appends a full reset even when the input had no styling.
        return text.slice(0, index) + '\x1b[0m';
    }
    const truncated = truncateToWidth(text, width, '');
    return truncated + ' '.repeat(Math.max(0, width - measureWidth(truncated)));
};

export const replaceVisibleContent = (line: string, content: string): string => (line.match(/^(?:\x1b\]133;[ABC]\x07)*/)?.[0] ?? '') + content;
