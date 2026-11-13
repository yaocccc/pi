// Pure border colors shared by active extensions; no legacy UI hooks.
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
