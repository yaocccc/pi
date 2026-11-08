import { truncateToWidth, visibleWidth } from '@earendil-works/pi-tui';

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
    const middle = label ? frameBorder('─ ', 'top', 'tool') + label + frameBorder(' ' + '─'.repeat(width - visibleWidth(label) - 5), 'top', 'tool')
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

export const padToWidth = (text: string, width: number): string => {
    const truncated = truncateToWidth(text, width, '');
    return truncated + ' '.repeat(Math.max(0, width - visibleWidth(truncated)));
};

export const replaceVisibleContent = (line: string, content: string): string => (line.match(/^(?:\x1b\]133;[ABC]\x07)*/)?.[0] ?? '') + content;
